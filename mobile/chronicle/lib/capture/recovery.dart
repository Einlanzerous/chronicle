/// What happens to a capture whose recorder did not get to finish.
///
/// ## The oracle this does NOT use
///
/// The obvious rule is "`state: recording` on disk at launch means an
/// interrupted capture". **It is wrong, and wrong in the direction this whole
/// ticket exists to prevent.** The capture service is a foreground service
/// precisely so that it outlives the UI: swipe the app out of Recents and the
/// recorder keeps going under its notification. Next launch, that rule calls a
/// live capture orphaned and trims a file still being appended to — silent loss
/// of authored audio, produced by the recovery code.
///
/// ## What it uses instead, in order
///
/// 1. **Ask the owner.** `CaptureService` runs in this app's own process — the
///    manifest declares no `android:process` — so its registry of held captures
///    answers immediately, with no IPC and no deadline. A force-stopped process
///    comes back with an empty registry, so "nobody holds this" is a *definite*
///    answer rather than a timeout, and recovery after a kill needs no wait at
///    all.
/// 2. **Fall back to the lease** only when the owner cannot be asked. A lease
///    older than three heartbeats means the recorder that wrote it is gone.
/// 3. **Re-look, rather than defer.** A lease too young to call, with no owner
///    to ask, schedules another look at lease-age plus a margin. Waiting for the
///    next launch would be waiting until tomorrow.
///
/// ## Nothing here deletes anything
///
/// Not the original, not a remnant, not a capture that recovered no audio at
/// all. The trim writes a *new* file and leaves `audio.opus` byte-identical:
/// the trim is the one operation in this ticket that could destroy authored
/// bytes, it runs exactly when something has already gone wrong, and an
/// off-by-one in the page arithmetic would be silent — a slightly shorter memo
/// that still probes clean. CHRN-20 §4 also rests on the client keeping its
/// copy: *"the phone still holds the file"* is what makes `tier1.memo_uploads`
/// honestly tier 1, and a client that deletes early makes that call wrong.
library;

import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

import 'capture_record.dart';
import 'ogg.dart';

/// Whoever can say, right now, whether a recorder still holds a capture.
abstract class CaptureOwner {
  /// True when a live recorder in this process holds [captureId].
  ///
  /// Throws when the owner cannot be reached at all, which is a different
  /// answer from `false` and is treated differently.
  Future<bool> isHeld(String captureId);
}

/// What one recovery pass did.
class RecoveryOutcome {
  const RecoveryOutcome({
    required this.salvaged,
    required this.empty,
    required this.stillRecording,
    required this.retryAfter,
  });

  final List<String> salvaged;

  /// Captures whose audio never reached the file. Kept, never sent.
  final List<String> empty;

  final List<String> stillRecording;

  /// Set when something could not be classified yet and deserves another look.
  final Duration? retryAfter;

  bool get isQuiet =>
      salvaged.isEmpty && empty.isEmpty && retryAfter == null;
}

/// Scans every capture under [root] and finishes the ones nobody is writing.
Future<RecoveryOutcome> recoverAll(
  Directory root,
  CaptureOwner owner, {
  DateTime? now,
}) async {
  final at = now ?? DateTime.now();
  final salvaged = <String>[];
  final empty = <String>[];
  final live = <String>[];
  Duration? retryAfter;

  if (!await root.exists()) {
    return const RecoveryOutcome(
      salvaged: [],
      empty: [],
      stillRecording: [],
      retryAfter: null,
    );
  }

  await for (final entry in root.list()) {
    if (entry is! Directory) continue;
    final captureId = entry.path.split(Platform.pathSeparator).last;
    final capture = CaptureDir(root, captureId);

    final record = await capture.readMeta();
    // No readable meta.json means no idempotency key and no started_at, so
    // there is nothing a queue could do with the bytes. Left alone rather than
    // removed: the audio, if any, is still on disk for a person to find.
    if (record == null) continue;
    if (record.state != CaptureState.recording) continue;

    bool? held;
    try {
      held = await owner.isHeld(captureId);
    } catch (_) {
      held = null; // the owner could not be asked; the lease arbitrates
    }

    if (held == true) {
      live.add(captureId);
      continue;
    }

    if (held == null) {
      final lease = await capture.readLease();
      if (lease != null && !lease.isStale(at)) {
        // Too young to call, and nobody to ask. Look again shortly rather than
        // at the next launch.
        final margin = Duration(milliseconds: lease.heartbeatMs);
        final age = at.difference(lease.heartbeatAt);
        final wait = Duration(milliseconds: lease.heartbeatMs * 3) - age + margin;
        retryAfter = _soonest(retryAfter, wait);
        continue;
      }
    }

    final outcome = await salvage(capture, record);
    (outcome.state == CaptureState.empty ? empty : salvaged).add(captureId);
  }

  return RecoveryOutcome(
    salvaged: salvaged,
    empty: empty,
    stillRecording: live,
    retryAfter: retryAfter,
  );
}

Duration? _soonest(Duration? a, Duration b) {
  final floor = b.isNegative ? Duration.zero : b;
  if (a == null) return floor;
  return floor < a ? floor : a;
}

/// Trims a torn capture to its last sound page and records what it found.
///
/// Writes `audio.trimmed`; never touches `audio.opus`.
Future<CaptureRecord> salvage(CaptureDir capture, CaptureRecord record) async {
  final bytes = await capture.audio.exists()
      ? Uint8List.fromList(await capture.audio.readAsBytes())
      : Uint8List(0);
  final scan = scanOgg(bytes);

  if (scan.isEmpty) {
    // Somebody spoke and none of it left the encoder. The remnant stays, and it
    // is shown with what IS known -- started_at and the last heartbeat bound the
    // duration -- so the person reads "recording of about 3 s, nothing
    // recovered" rather than finding that a memo they remember making is simply
    // not there.
    final updated = record.copyWith(
      state: CaptureState.empty,
      byteSize: bytes.length,
      trimOffset: scan.trimOffset,
    );
    await capture.writeMeta(updated);
    return updated;
  }

  final keep = Uint8List.sublistView(bytes, 0, scan.trimOffset);
  await capture.trimmed.writeAsBytes(keep, flush: true);

  final updated = record.copyWith(
    state: CaptureState.salvaged,
    contentHash: sha256.convert(keep).toString(),
    byteSize: keep.length,
    durationMs: scan.durationMs,
    trimOffset: scan.trimOffset,
  );
  await capture.writeMeta(updated);
  return updated;
}

/// Finishes a capture the recorder stopped cleanly.
///
/// A clean stop still goes through the page scan rather than trusting the file,
/// because the hash the server checks has to be taken over exactly the bytes
/// that will be sent, and because a recorder that failed at `stop()` can leave a
/// file that looks finished and is not.
Future<CaptureRecord> finalise(CaptureDir capture, CaptureRecord record) async {
  final bytes = await capture.audio.exists()
      ? Uint8List.fromList(await capture.audio.readAsBytes())
      : Uint8List(0);
  final scan = scanOgg(bytes);

  if (scan.isEmpty) {
    final updated = record.copyWith(
      state: CaptureState.empty,
      byteSize: bytes.length,
      trimOffset: scan.trimOffset,
    );
    await capture.writeMeta(updated);
    return updated;
  }

  // A clean stop that did not end on a page boundary is a torn file wearing a
  // finished file's clothes, and it takes the salvage path rather than being
  // sent as-is: the server would refuse a duration for it.
  if (!scan.endsAtEof) return salvage(capture, record);

  final updated = record.copyWith(
    state: CaptureState.ready,
    contentHash: sha256.convert(bytes).toString(),
    byteSize: bytes.length,
    durationMs: scan.durationMs,
  );
  await capture.writeMeta(updated);
  return updated;
}
