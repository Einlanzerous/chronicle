/// The queue engine: drives ONE pass over a set of captures against a
/// [UploadTransport], turning `failure.dart`'s classification and `ack.dart`'s
/// verification into the only writes this ticket makes to `upload.json`.
///
/// **Scope.** This is the protocol implementation from CHRN-61's plan,
/// section "One attempt, always the same shape" and "who wakes the queue" --
/// minus the "who wakes it" part. Nothing here decides WHEN to run; a caller
/// (the step-4 `queue_controller`) decides that and hands this engine a list
/// of captures. Nothing here reads a filesystem to discover captures either
/// -- that is also the caller's job, because listing `<filesDir>/captures/`
/// is unrelated to running the protocol against one.
///
/// **The escalation policy lives here, not in `failure.dart`.** Whether a
/// repeated [FailureClass] becomes [QueueStatus.rejected] needs the
/// capture's persisted history ([QueueRecord.failureStreak]), which a pure
/// classifier has no business holding. See [_escalates].
///
/// **Deliberately deferred to the screen (build step 4), not decided here:**
/// the "evidence" rule for the `NOT SENT — server error` label ("a 5xx only
/// advances the label when some OTHER capture has acknowledged since", plan
/// section "loose ends"/failure table). That turns out to need no engine
/// state at all -- every [QueueRecord] already on disk carries its own
/// `acknowledgedAt`, so "has anything else succeeded recently" is answerable
/// by scanning the same records the screen already reads, at render time.
/// Baking it into this engine would mean carrying cross-capture state for a
/// fact only the renderer needs.
library;

import 'dart:io';

import 'package:chronicle_api/api.dart';

import '../capture/capture_record.dart';
import 'ack.dart';
import 'backoff.dart';
import 'device_block.dart';
import 'failure.dart';
import 'queue_record.dart';
import 'retention_gate.dart';
import 'upload_transport.dart';

/// One capture, bundled with its queue state, ready for the engine to drive.
/// The controller is what assembles a list of these each pass -- reading
/// every capture directory's `meta.json` and `upload.json` is a filesystem
/// walk the engine itself has no reason to know how to do.
class QueueCapture {
  const QueueCapture({
    required this.queueDir,
    required this.capture,
    required this.queueRecord,
  });

  final QueueDir queueDir;
  final CaptureRecord capture;
  final QueueRecord queueRecord;
}

class QueueEngine {
  QueueEngine({
    required this.transport,
    this.chunkSize = 512 * 1024,
    this.maxConsecutiveFailures = 3,
    DateTime Function()? now,
  }) : _now = now ?? DateTime.now;

  final UploadTransport transport;

  /// Bytes per `PATCH`. 512 KiB, matching the plan; a test shrinks this to
  /// exercise the chunk loop over a small fixture file without minting
  /// megabytes of fixture audio.
  final int chunkSize;

  /// How many CONSECUTIVE attempts of the same escalating [FailureClass]
  /// park a capture as `rejected`. Only [FailureClass.noProgress],
  /// [FailureClass.protocolOversend] and [FailureClass.rejectedOther]
  /// escalate at all -- see [_escalates] and each class's own doc comment
  /// in `queue_record.dart`.
  final int maxConsecutiveFailures;

  final DateTime Function() _now;

  /// Drains [captures], oldest [CaptureRecord.startedAt] first, one at a
  /// time -- sequential by design, so one slow chunk never contends with
  /// another capture's send. Returns the [DeviceBlock] now in effect, or
  /// null if the pass ran to completion with no device-level refusal.
  ///
  /// Stops the moment an outcome says nothing else will work either: no
  /// network at all, or a device block (signed out / wrong host). A
  /// capture-specific outcome -- an ack, a rejection, a transient 5xx, a
  /// file that no longer matches its declared length -- never stops the
  /// pass; the next capture is still worth trying.
  ///
  /// [token], [serverUrl] and [tokenDigest] are the device's current
  /// session facts, supplied by the caller (never read from storage here --
  /// see `device_block.dart`'s own rule about persisting device facts).
  /// [currentBlock] is honoured first: a block that still applies ends the
  /// pass before a single request is made.
  ///
  /// [onAttemptStart], if given, is called with a capture's id right before
  /// its attempt begins and nowhere else -- the one place this engine
  /// exposes the live fact the plan's screen label needs (`SENDING`, next
  /// to the persisted labels `QUEUED`/`SENT`/etc.), without persisting a
  /// `sending` flag anywhere. Purely observational: nothing here waits on
  /// it or changes behaviour because of it.
  Future<DeviceBlock?> drainPass({
    required List<QueueCapture> captures,
    required String? token,
    required String serverUrl,
    required String tokenDigest,
    DeviceBlock? currentBlock,
    void Function(String captureId)? onAttemptStart,
  }) async {
    if (currentBlock != null &&
        currentBlock.stillApplies(
          serverUrl: serverUrl,
          tokenDigest: tokenDigest,
        )) {
      return currentBlock;
    }
    if (token == null || token.isEmpty) {
      return DeviceBlock(
        reason: DeviceBlockReason.signedOut,
        serverUrl: serverUrl,
        tokenDigest: tokenDigest,
      );
    }

    final now = _now();
    final ordered = [...captures]
      ..sort((a, b) => a.capture.startedAt.compareTo(b.capture.startedAt));

    for (final qc in ordered) {
      if (qc.queueRecord.status == QueueStatus.acknowledged ||
          qc.queueRecord.status == QueueStatus.rejected) {
        continue;
      }
      if (!retentionGateOpen(
        retention: qc.capture.retention,
        enqueuedAt: qc.queueRecord.enqueuedAt,
        now: now,
      )) {
        continue;
      }
      if (!backoffElapsed(
        captureId: qc.capture.captureId,
        attemptCount: qc.queueRecord.attemptCount,
        lastAttemptAt: qc.queueRecord.lastAttemptAt,
        now: now,
      )) {
        continue;
      }

      onAttemptStart?.call(qc.capture.captureId);
      final outcome = await _attemptOne(qc);

      if (outcome is _Skip) {
        continue;
      }
      if (outcome is _FileChanged) {
        await _writeUnlessAcknowledged(
          qc.queueDir,
          qc.queueRecord.copyWith(status: QueueStatus.blockedLocalFileChanged),
        );
        continue;
      }
      if (outcome is _Acked) {
        await _writeUnlessAcknowledged(qc.queueDir, _applyAck(qc.queueRecord, outcome.ack, now));
        continue;
      }
      if (outcome is _CaptureFailed) {
        await _writeUnlessAcknowledged(
          qc.queueDir,
          _applyFailure(qc.queueRecord, outcome.failure, now),
        );
        continue;
      }

      // The only case left: a pass-ending refusal -- no network at all, or
      // a device block. Nothing else in this pass will fare any better.
      final passEnds = (outcome as _PassEnded).passEnds;
      final captureClass = passEnds.captureFailureClass;
      if (captureClass != null) {
        // A network failure is still recorded against the capture that hit
        // it -- backed off like any other capture failure -- even though
        // the pass itself stops here.
        await _writeUnlessAcknowledged(
          qc.queueDir,
          _applyFailure(qc.queueRecord, CaptureFailure(captureClass), now),
        );
      }
      if (passEnds.reason == PassEndReason.network) return currentBlock;
      return DeviceBlock(
        reason: passEnds.reason == PassEndReason.wrongHost
            ? DeviceBlockReason.wrongHost
            : DeviceBlockReason.signedOut,
        serverUrl: serverUrl,
        tokenDigest: tokenDigest,
      );
    }
    return null;
  }

  /// One capture's whole attempt: open, then chunks until an ack or a
  /// failure. Never writes anything -- [drainPass] is the only writer, once
  /// this returns, so a capture is never left in a state this method itself
  /// did not fully decide.
  Future<_AttemptOutcome> _attemptOne(QueueCapture qc) async {
    final capture = qc.capture;

    if (capture.state != CaptureState.ready &&
        capture.state != CaptureState.salvaged) {
      // `recording` (still being written -- never touch a live file) and
      // `empty` (visible, but nothing to send) are not queue material. The
      // controller should not be handing these over; the engine does not
      // trust that from outside its own package.
      return const _Skip();
    }

    final expectedSize = capture.byteSize;
    final expectedHash = capture.contentHash;
    if (expectedSize == null || expectedHash == null) {
      // A sendable state always carries both by the time capture/recovery
      // marks it so. If a record slips through without them, "not ready
      // yet" is the honest reading, not a failure.
      return const _Skip();
    }

    final sendable = qc.queueDir.capture.sendable(capture.state);
    int actualLength;
    try {
      actualLength = await sendable.length();
    } on FileSystemException {
      return const _FileChanged();
    }
    if (actualLength != expectedSize) {
      return const _FileChanged();
    }

    UploadState opened;
    try {
      opened = await transport.openUpload(
        idempotencyKey: capture.idempotencyKey,
        contentHash: expectedHash,
        byteSize: expectedSize,
        recordedAt: capture.startedAt,
        retention: capture.retention,
      );
    } catch (e) {
      return _fromOutcome(classifyError(e));
    }

    final openedAck = verifyAck(opened, capture);
    if (openedAck != null) return _Acked(openedAck);
    if (opened.status != UploadStateStatusEnum.incomplete) {
      return _fromOutcome(unverifiedAck);
    }
    final uploadId = opened.uploadId;
    if (uploadId == null || uploadId.isEmpty) {
      return _fromOutcome(unverifiedAck);
    }
    var offset = opened.offset;
    if (offset < 0 || offset > expectedSize) {
      // Never trust a server-stated offset past the file's own length --
      // reading past [expectedSize] below would send garbage. Treated like
      // any other ambiguous, non-acking answer: safe to retry.
      return _fromOutcome(unverifiedAck);
    }

    // The same file the length check above already tolerated losing. It can
    // vanish between the two lines: the other isolate's prune pass
    // (`prune.dart`) deletes an ACKNOWLEDGED capture's audio, and this
    // attempt may be working from a snapshot taken before that ack. Read as
    // "the file changed"; `_writeUnlessAcknowledged` then declines to write
    // over the `acknowledged` the other isolate already recorded.
    final List<int> bytes;
    try {
      bytes = await sendable.readAsBytes();
    } on FileSystemException {
      return const _FileChanged();
    }
    while (offset < expectedSize) {
      final end =
          (offset + chunkSize < expectedSize) ? offset + chunkSize : expectedSize;
      final sentFrom = offset;
      final chunk = bytes.sublist(sentFrom, end);

      UploadState next;
      try {
        next = await transport.appendChunk(
          uploadId: uploadId,
          offset: sentFrom,
          bytes: chunk,
        );
      } catch (e) {
        final outcome = classifyError(e, sentFromOffset: sentFrom);
        if (outcome is Resume) {
          offset = outcome.offset;
          continue;
        }
        return _fromOutcome(outcome);
      }

      final chunkAck = verifyAck(next, capture);
      if (chunkAck != null) return _Acked(chunkAck);
      if (next.status != UploadStateStatusEnum.incomplete) {
        return _fromOutcome(unverifiedAck);
      }
      if (next.offset <= sentFrom) {
        // The same "no hot loop against a repeating non-advance" guard
        // `classifyResponse` applies to an ERROR answer (see
        // `_resumeOrNoProgress`), applied here to a 200 that claims success
        // but reports no forward progress.
        return const _CaptureFailed(CaptureFailure(FailureClass.noProgress));
      }
      offset = next.offset;
    }
    // Every declared byte sent and the server never said `complete`: the
    // exact ambiguous case this ticket exists for. Safe default: try again
    // next pass, where re-opening resolves it (`duplicate: true` if it
    // actually landed).
    return _fromOutcome(unverifiedAck);
  }

  /// Writes [record] UNLESS the file already says `acknowledged` -- read
  /// fresh, right before the write, not trusted from [dir]'s caller's
  /// stale snapshot.
  ///
  /// This is what keeps two engines racing the same capture safe with the
  /// exclusion optimisation disabled (`IsolateNameServer`, "who wakes the
  /// queue" in the plan): [record] here was decided from an [Outcome] this
  /// engine's OWN attempt produced, which for the losing side of a race is
  /// a stale, non-acking one (a 409 resync, an offset that no longer lines
  /// up) -- computed correctly from THIS attempt, but by the time it is
  /// about to be written, the OTHER engine may already have written a
  /// verified `acknowledged` for the same capture. Without this guard the
  /// loser's stale, honestly-non-acking write would overwrite that ack with
  /// `pending`, a real memo silently "hidden" from the local record until
  /// the next pass re-opens and re-discovers it (`duplicate: true`) --
  /// never a false ack, never a second memo, but also never necessary.
  /// `acknowledged` is terminal; nothing this engine ever produces should
  /// un-acknowledge it.
  Future<void> _writeUnlessAcknowledged(QueueDir dir, QueueRecord record) async {
    final onDisk = await dir.read();
    if (onDisk?.status == QueueStatus.acknowledged) return;
    await dir.write(record);
  }

  QueueRecord _applyAck(QueueRecord prior, Ack ack, DateTime now) => prior.copyWith(
        status: QueueStatus.acknowledged,
        attemptCount: prior.attemptCount + 1,
        lastAttemptAt: now,
        clearLastFailureClass: true,
        clearLastFailureCode: true,
        failureStreak: 0,
        clearRejectReason: true,
        memoId: ack.memoId,
        acknowledgedAt: now,
      );

  QueueRecord _applyFailure(QueueRecord prior, CaptureFailure failure, DateTime now) {
    final streak =
        prior.lastFailureClass == failure.failureClass ? prior.failureStreak + 1 : 1;
    final parks = failure.parkImmediately ||
        (_escalates(failure.failureClass) && streak >= maxConsecutiveFailures);
    return prior.copyWith(
      status: parks ? QueueStatus.rejected : QueueStatus.pending,
      attemptCount: prior.attemptCount + 1,
      lastAttemptAt: now,
      lastFailureClass: failure.failureClass,
      lastFailureCode: failure.code,
      clearLastFailureCode: failure.code == null,
      failureStreak: streak,
      rejectReason: parks ? _rejectReasonFor(failure.failureClass) : null,
      clearRejectReason: !parks,
    );
  }
}

/// The classes that escalate to `rejected` after `maxConsecutiveFailures`
/// CONSECUTIVE occurrences: something about the protocol exchange itself is
/// wrong, not the network and not a passing server hiccup.
/// [FailureClass.network] and [FailureClass.transientServer] are
/// deliberately absent -- never escalate on their own, per their own doc
/// comments in `queue_record.dart`.
bool _escalates(FailureClass c) =>
    c == FailureClass.noProgress ||
    c == FailureClass.protocolOversend ||
    c == FailureClass.rejectedOther;

RejectReason _rejectReasonFor(FailureClass c) {
  if (c == FailureClass.rejectedKeyReused) return RejectReason.keyReused;
  if (c == FailureClass.rejectedHashMismatch) return RejectReason.hashMismatch;
  if (c == FailureClass.rejectedOther) return RejectReason.serverRefused;
  // The only classes left that ever park: noProgress, protocolOversend.
  // network/transientServer never reach here -- _escalates is false for
  // both, and parkImmediately is never true for either.
  return RejectReason.protocol;
}

/// What one capture's attempt decided. Private to this file: nothing
/// outside `drainPass` needs to know the difference between "not sendable
/// yet" and "the file changed under us" -- both mean "wrote nothing, moved
/// on", one of them just also updates the record.
sealed class _AttemptOutcome {
  const _AttemptOutcome();
}

/// Not queue material this pass: still recording, empty, or not yet hashed.
/// No write -- there is nothing to say that isn't already implied by the
/// capture's own `meta.json` state.
class _Skip extends _AttemptOutcome {
  const _Skip();
}

/// The sendable file's length no longer matches what was declared.
class _FileChanged extends _AttemptOutcome {
  const _FileChanged();
}

class _Acked extends _AttemptOutcome {
  const _Acked(this.ack);
  final Ack ack;
}

class _CaptureFailed extends _AttemptOutcome {
  const _CaptureFailed(this.failure);
  final CaptureFailure failure;
}

class _PassEnded extends _AttemptOutcome {
  const _PassEnded(this.passEnds);
  final PassEnds passEnds;
}

_AttemptOutcome _fromOutcome(Outcome outcome) {
  if (outcome is CaptureFailure) return _CaptureFailed(outcome);
  if (outcome is PassEnds) return _PassEnded(outcome);
  // Resume is consumed inside the chunk loop and must never reach here.
  throw StateError('unexpected terminal Outcome: $outcome');
}
