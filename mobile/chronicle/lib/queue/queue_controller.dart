/// Foreground wiring for CHRN-61's upload queue: WHEN the engine runs, never
/// HOW -- that is `engine.dart`'s job, exercised end to end against a fake
/// server in `test/queue/engine_test.dart`. This controller answers the
/// plan's "who wakes the queue" for the IN-APP triggers only (launch, a
/// capture reaching `ready`, app resume, backoff, and a manual retry); the
/// process-dead trigger is build step 5 (WorkManager), a headless isolate
/// that does not go through Riverpod at all.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:crypto/crypto.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/providers.dart';
import '../api/server_url.dart';
import '../api/session.dart';
import '../auth/auth_controller.dart';
import '../capture/capture_channel.dart';
import '../capture/capture_controller.dart';
import '../capture/capture_record.dart';
import 'backoff.dart';
import 'device_block.dart';
import 'engine.dart';
import 'queue_record.dart';
import 'upload_transport.dart';
import 'uploads_api_transport.dart';

/// What the screen and the RECENT chips read: one [QueueRecord] per capture
/// id, plus the device block in effect, if any.
///
/// A capture absent from [records] is one the queue has not scanned yet --
/// still `recording`, or a pass has not run since it appeared. Nothing here
/// is a live "sending" flag; see `queue_record.dart`'s own rule that
/// `uploading` is a fact of a running engine and is never persisted, which
/// this state does not invent a UI-only copy of either.
class QueueUiState {
  const QueueUiState({this.records = const {}, this.deviceBlock, this.sending});

  final Map<String, QueueRecord> records;
  final DeviceBlock? deviceBlock;

  /// The one capture currently mid-attempt, if any -- `drainPass` is
  /// sequential, so there is never more than one. Cleared the moment a
  /// pass ends, whatever it decided; this is the SCREEN's `SENDING` label
  /// and the only field on this state that is not sourced from a
  /// [QueueRecord].
  final String? sending;

  QueueUiState copyWith({
    Map<String, QueueRecord>? records,
    DeviceBlock? deviceBlock,
    bool clearDeviceBlock = false,
    String? sending,
    bool clearSending = false,
  }) =>
      QueueUiState(
        records: records ?? this.records,
        deviceBlock: clearDeviceBlock ? null : (deviceBlock ?? this.deviceBlock),
        sending: clearSending ? null : (sending ?? this.sending),
      );
}

/// The real transport, watching the same credential/address every other API
/// surface does.
final uploadTransportProvider = Provider<UploadTransport>(
  (ref) => UploadsApiTransport(ref.watch(uploadsApiProvider)),
);

final queueEngineProvider = Provider<QueueEngine>(
  (ref) => QueueEngine(transport: ref.watch(uploadTransportProvider)),
);

class QueueController extends Notifier<QueueUiState> {
  Timer? _retryTimer;
  AppLifecycleListener? _lifecycle;
  Future<void>? _inFlight;

  late final CapturePlatform _platform = ref.read(capturePlatformProvider);

  @override
  QueueUiState build() {
    ref.onDispose(() {
      _retryTimer?.cancel();
      _lifecycle?.dispose();
    });

    // A capture reaching `ready`/`salvaged`, or recovery resolving one, is
    // read off here rather than watched directly: `CaptureController.refresh`
    // always installs a FRESH `List` instance, so this fires on every
    // refresh regardless of whether the contents actually changed -- cheap
    // when there is nothing new to send, correct when there is.
    ref.listen(captureControllerProvider.select((s) => s.recent), (_, _) {
      unawaited(wake());
    });

    // A sign-in, a sign-out, or a re-scanned address is exactly the event
    // that can make a `signedOut`/`wrongHost` block stop applying (its
    // `stillApplies` check is keyed on these two facts) -- and it is the
    // acceptance criterion in the plan's own words: "after sign-in the
    // queue resumes unprompted."
    ref.listen(sessionTokenProvider, (_, _) => unawaited(wake()));
    ref.listen(serverUrlProvider, (_, _) => unawaited(wake()));

    _lifecycle = AppLifecycleListener(onResume: () => unawaited(wake()));

    return const QueueUiState();
  }

  /// One pass: scan every capture directory, drive the engine, persist what
  /// it decided, and schedule the next backoff-bound wake if anything is
  /// still waiting. Concurrent calls coalesce onto whichever pass is
  /// already running rather than starting a second one over the same
  /// files.
  Future<void> wake() {
    final existing = _inFlight;
    if (existing != null) return existing;
    final future = _wakeOnce(allowArbitrationRetry: true);
    _inFlight = future;
    future.whenComplete(() {
      if (identical(_inFlight, future)) _inFlight = null;
    });
    return future;
  }

  /// [allowArbitrationRetry] bounds the one recursive case below to depth
  /// one: a confirmed-good session retries immediately, once. Without the
  /// bound, a capture whose SEND endpoint keeps 401ing despite `/auth/me`
  /// insisting the session is fine -- an unlikely but real possible
  /// disagreement between two endpoints -- would recurse forever instead
  /// of settling into an ordinary, re-triggerable block.
  Future<void> _wakeOnce({required bool allowArbitrationRetry}) async {
    final root = await _platform.capturesRoot();
    if (!await root.exists() || !ref.mounted) return;

    final now = DateTime.now();
    final captures = <QueueCapture>[];
    await for (final entry in root.list()) {
      if (entry is! Directory) continue;
      final id = entry.path.split(Platform.pathSeparator).last;
      final captureDir = CaptureDir(root, id);
      final capture = await captureDir.readMeta();
      if (capture == null || capture.state == CaptureState.recording) continue;
      final queueDir = QueueDir(captureDir);
      final record = await _ensureEnqueued(queueDir, now);
      captures.add(QueueCapture(queueDir: queueDir, capture: capture, queueRecord: record));
    }
    if (!ref.mounted) return;

    final token = ref.read(sessionTokenProvider);
    final serverUrl = ref.read(serverUrlProvider);
    final wasBlockedSignedOut = state.deviceBlock?.reason == DeviceBlockReason.signedOut;
    final block = await ref.read(queueEngineProvider).drainPass(
          captures: captures,
          token: token,
          serverUrl: serverUrl,
          tokenDigest: _digest(token),
          currentBlock: state.deviceBlock,
          onAttemptStart: (id) {
            if (ref.mounted) state = state.copyWith(sending: id);
          },
        );
    if (!ref.mounted) return;

    final records = <String, QueueRecord>{};
    for (final qc in captures) {
      records[qc.capture.captureId] = await qc.queueDir.read() ?? qc.queueRecord;
    }
    if (!ref.mounted) return;

    state = state.copyWith(
      records: records,
      deviceBlock: block,
      clearDeviceBlock: block == null,
      clearSending: true,
    );

    if (block?.reason == DeviceBlockReason.signedOut && !wasBlockedSignedOut) {
      // Arbitrated by `/auth/me`, never by the queue writing the credential
      // store directly (`device_block.dart`'s own rule): a stale or
      // wrong-endpoint 401 must not be trusted over the sign-out path that
      // already exists. Invalidated and read directly here -- a standing
      // `ref.listen(meProvider, ...)` would also fire on meProvider's own
      // ordinary rebuilds (a cold first read, ANY other reason it
      // recomputes) and could lift a block that answer had nothing to do
      // with. Only on the FIRST pass into this block, not on every pass it
      // persists through.
      ref.invalidate(meProvider);
      final confirmed = await ref.read(meProvider.future).catchError((_) => null);
      if (!ref.mounted) return;
      if (confirmed != null) {
        // The session is genuinely still good: a transient or
        // wrong-endpoint 401, not a real sign-out. Lift the block just
        // raised and retry immediately -- calling `_wakeOnce` directly,
        // never the public `wake()`, which would just coalesce onto THIS
        // still-running call and do nothing.
        state = state.copyWith(clearDeviceBlock: true);
        if (allowArbitrationRetry) {
          await _wakeOnce(allowArbitrationRetry: false);
        }
        return;
      }
    }

    _scheduleNextWake(records.values);
  }

  /// Moves a `rejected` capture back to `pending` -- the only way one ever
  /// moves, per `queue_record.dart`'s own rule that the engine never
  /// retries a rejection on its own.
  Future<void> retryCapture(String captureId) async {
    final root = await _platform.capturesRoot();
    final queueDir = QueueDir(CaptureDir(root, captureId));
    final record = await queueDir.read();
    if (record == null || record.status != QueueStatus.rejected) return;
    await queueDir.write(record.copyWith(
      status: QueueStatus.pending,
      clearRejectReason: true,
      clearLastFailureClass: true,
      clearLastFailureCode: true,
      // Resetting the streak but leaving `lastAttemptAt` in place would
      // have this retry sit out the SAME backoff it exists to override --
      // `backoffElapsed` reads only the timestamp, not the streak.
      clearLastAttemptAt: true,
      attemptCount: 0,
      failureStreak: 0,
    ));
    await wake();
  }

  /// The moment the queue first sees a capture sendable, persisted at once
  /// -- never recomputed on a later pass. `retention_gate.dart`'s grace
  /// clock runs from this timestamp, and recomputing it on every `wake()`
  /// for a capture that has not been written yet (still retention-gated,
  /// say) would mean the clock never actually starts.
  Future<QueueRecord> _ensureEnqueued(QueueDir dir, DateTime now) async {
    final existing = await dir.read();
    if (existing != null) return existing;
    final fresh = QueueRecord.fresh(now);
    await dir.write(fresh);
    return fresh;
  }

  /// Backoff is per-capture and jittered (`backoff.dart`); this schedules
  /// the pass itself for the EARLIEST capture's un-jittered threshold,
  /// which can only fire early relative to any one capture's own jittered
  /// instant, never late -- `drainPass`'s own `backoffElapsed` check is
  /// still the authority, so an early wake just finds nothing eligible yet
  /// and reschedules.
  void _scheduleNextWake(Iterable<QueueRecord> records) {
    _retryTimer?.cancel();
    // A device block lifts via a sign-in, a re-scan, or `/auth/me`
    // confirming the session (the three listeners above) -- never a timer.
    if (state.deviceBlock != null) return;

    DateTime? earliest;
    for (final r in records) {
      if (r.status != QueueStatus.pending || r.lastAttemptAt == null) continue;
      final at = r.lastAttemptAt!.add(backoffDelay(r.attemptCount));
      final soFar = earliest;
      if (soFar == null || at.isBefore(soFar)) earliest = at;
    }
    final threshold = earliest;
    if (threshold == null) return;

    final wait = threshold.difference(DateTime.now());
    _retryTimer = Timer(wait.isNegative ? Duration.zero : wait, () => unawaited(wake()));
  }

  /// A digest of the held token, never the token itself -- see
  /// `device_block.dart`'s own rule for why.
  static String _digest(String? token) {
    if (token == null || token.isEmpty) return '';
    return sha256.convert(utf8.encode(token)).toString();
  }
}

final queueControllerProvider =
    NotifierProvider<QueueController, QueueUiState>(QueueController.new);
