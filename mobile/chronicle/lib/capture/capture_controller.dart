/// One tap to record, and everything that has to be true around it.
library;

import 'dart:async';
import 'dart:io';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'capture_channel.dart';
import 'capture_record.dart';
import 'recovery.dart';

/// Leave this much of the phone free whatever happens.
///
/// A recorder that fills the last block takes the rest of the system down with
/// it, and the memo it was saving is not worth that.
const int _reserveBytes = 256 * 1024 * 1024;

/// Plus an hour of headroom, so a refusal means "not enough for a memo" rather
/// than "not enough for another second".
///
/// Measured rather than assumed: a 13.2 s device recording came to 53,769
/// bytes, which is ~4.1 kB/s, so an hour of mono Opus is about 15 MB. (The
/// canvas's `LOCAL BUFFER · 2.1 MB` at `00:47` is decorative — the real figure
/// at that point is nearer 190 kB.)
const int _hourOfAudioBytes = 16 * 1024 * 1024;

const int _minFreeBytes = _reserveBytes + _hourOfAudioBytes;

/// Below this much captured audio, a hold is treated as a tap.
///
/// **Measured on audio captured, never on how long the finger was down.** Audio
/// does not start until the microphone actually opens, so a 450 ms hold can
/// yield 150 ms of sound; a rule about the finger would call that a deliberate
/// hold and end a memo that never really began. This way the rule is about the
/// thing it is protecting.
const int _latchBelowMs = 400;

class CaptureUiState {
  const CaptureUiState({
    this.recorder = RecorderSnapshot.idle,
    this.current,
    this.recent = const [],
    this.refusal,
    this.latched = false,
  });

  final RecorderSnapshot recorder;

  /// The record for the capture being written, if any.
  final CaptureRecord? current;

  /// Finished captures, newest first. Local only until CHRN-61 and CHRN-63.
  final List<CaptureRecord> recent;

  /// Why the last attempt to start was refused, in words with a number in them.
  final String? refusal;

  /// A hold that was too short, or was cancelled, has latched into
  /// tap-to-talk: the recording continues and a tap ends it.
  final bool latched;

  bool get isRecording => recorder.state != RecorderState.idle;

  CaptureUiState copyWith({
    RecorderSnapshot? recorder,
    CaptureRecord? current,
    List<CaptureRecord>? recent,
    String? refusal,
    bool clearRefusal = false,
    bool clearCurrent = false,
    bool? latched,
  }) =>
      CaptureUiState(
        recorder: recorder ?? this.recorder,
        current: clearCurrent ? null : (current ?? this.current),
        recent: recent ?? this.recent,
        refusal: clearRefusal ? null : (refusal ?? this.refusal),
        latched: latched ?? this.latched,
      );
}

class CaptureController extends Notifier<CaptureUiState> {
  StreamSubscription<RecorderSnapshot>? _sub;
  Timer? _reLook;

  /// A finger is down on the capture control and a start is in flight.
  ///
  /// **The gesture cannot key off `isRecording`.** Starting is asynchronous --
  /// permission, a free-space check, writing `meta.json`, then the service --
  /// and a quick tap's pointer-up lands long before any of that reports back.
  /// Gating the release on "are we recording yet" therefore dropped the release
  /// entirely for exactly the shortest gestures, which are the ones the latch
  /// rule exists to catch. Found on device.
  bool _holdPending = false;

  CapturePlatform get _platform => ref.read(capturePlatformProvider);

  @override
  CaptureUiState build() {
    ref.onDispose(() {
      _sub?.cancel();
      _reLook?.cancel();
    });
    // The UI reads the recorder's state rather than remembering its own, which
    // is what lets a recreated UI re-attach to a recording already in flight
    // instead of showing idle over the top of one.
    _sub = _platform.watch().listen((snapshot) {
      state = state.copyWith(recorder: snapshot);
    });
    unawaited(recover());
    return const CaptureUiState();
  }

  /// Finishes anything a previous run left behind, then refreshes the list.
  Future<void> recover() async {
    final root = await _platform.capturesRoot();
    final outcome = await recoverAll(root, const PlatformCaptureOwner());

    _reLook?.cancel();
    final wait = outcome.retryAfter;
    if (wait != null) {
      // Something was too young to classify and had no owner to ask. Look again
      // shortly; waiting for the next launch could mean waiting until tomorrow.
      _reLook = Timer(wait, () => unawaited(recover()));
    }
    await refresh();
  }

  /// Reloads the finished captures on disk, newest first.
  Future<void> refresh() async {
    final root = await _platform.capturesRoot();
    if (!await root.exists()) return;
    final records = <CaptureRecord>[];
    await for (final entry in root.list()) {
      if (entry is! Directory) continue;
      final id = entry.path.split(Platform.pathSeparator).last;
      final record = await CaptureDir(root, id).readMeta();
      if (record != null && record.state != CaptureState.recording) {
        records.add(record);
      }
    }
    records.sort((a, b) => b.startedAt.compareTo(a.startedAt));
    state = state.copyWith(recent: records);
  }

  /// The idle screen's primary control: start, or stop if already running.
  Future<void> toggle() async {
    if (state.isRecording) {
      await stop();
    } else {
      await _start(latched: true);
    }
  }

  /// Finger down on the capture control.
  Future<void> beginHold() {
    _holdPending = true;
    return _start(latched: false);
  }

  /// Finger lifted.
  ///
  /// A hold that captured less than [_latchBelowMs] of audio latches instead of
  /// stopping, so a brush against the control cannot produce a 200 ms memo that
  /// then needs triaging.
  Future<void> endHold() async {
    if (state.latched) return;

    // The finger lifted before the recorder even reported in. That is by
    // definition less than the threshold, so it latches -- and it must be
    // handled here rather than ignored, or the shortest gestures fall through
    // every rule and leave the controller believing a finger is still down.
    if (!state.isRecording) {
      if (_holdPending) {
        _holdPending = false;
        state = state.copyWith(latched: true);
      }
      return;
    }

    _holdPending = false;
    if (state.recorder.elapsedMs < _latchBelowMs) {
      state = state.copyWith(latched: true);
      return;
    }
    await stop();
  }

  /// The gesture was cancelled by the system rather than by the person.
  ///
  /// **A cancel is not a release.** "Release stops" is the only gesture in this
  /// app that ends a recording without anybody deciding to, and the system
  /// cancels a pointer for reasons that have nothing to do with the finger — a
  /// system gesture, palm rejection, the notification shade coming down, a call
  /// ringing. Every one of those would otherwise end a memo mid-thought, so a
  /// cancel keeps recording and latches.
  void cancelHold() {
    if (!state.isRecording && !_holdPending) return;
    _holdPending = false;
    state = state.copyWith(latched: true);
  }

  Future<void> _start({required bool latched}) async {
    if (state.isRecording) return;
    state = state.copyWith(clearRefusal: true);

    if (!await _platform.requestPermissions()) {
      state = state.copyWith(
        refusal: 'Chronicle cannot record without the microphone. '
            'Grant it in Settings and try again.',
      );
      return;
    }

    final free = await _platform.freeBytes();
    if (free < _minFreeBytes) {
      // Named with the actual number, because a refusal somebody can act on
      // beats a recording that dies at minute nine.
      state = state.copyWith(
        refusal: 'Not enough space to record safely — '
            '${_mb(free)} free, ${_mb(_minFreeBytes)} needed.',
      );
      return;
    }

    final identity = CaptureIdentity.mint();
    final root = await _platform.capturesRoot();
    final capture = CaptureDir(root, identity.captureId);

    // Written and flushed BEFORE the recorder starts. CHRN-20 asks only that
    // the idempotency key be persisted before the request goes out; persisting
    // it before the first audio byte is stricter and free, and it means a
    // capture that survives a crash carries the key it would have had.
    final record = CaptureRecord(
      captureId: identity.captureId,
      idempotencyKey: identity.idempotencyKey,
      startedAt: DateTime.now(),
      state: CaptureState.recording,
    );
    await capture.writeMeta(record);

    await _platform.start(identity.captureId);
    state = state.copyWith(current: record, latched: latched);
  }

  Future<void> stop() async {
    if (!state.isRecording) return;
    final record = state.current;
    await _platform.stop();

    if (record != null) {
      final root = await _platform.capturesRoot();
      await finalise(CaptureDir(root, record.captureId), record);
    }
    _holdPending = false;
    state = state.copyWith(clearCurrent: true, latched: false);
    await refresh();
  }

  Future<void> pause() => _platform.pause();

  Future<void> resume() => _platform.resume();

  static String _mb(int bytes) => '${(bytes / (1024 * 1024)).round()} MB';
}

final capturePlatformProvider =
    Provider<CapturePlatform>((ref) => const CapturePlatform());

final captureControllerProvider =
    NotifierProvider<CaptureController, CaptureUiState>(CaptureController.new);
