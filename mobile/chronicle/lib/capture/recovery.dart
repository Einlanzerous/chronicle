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
    this.ready = const [],
    this.salvaged = const [],
    this.empty = const [],
    this.stillRecording = const [],
    this.retryAfter,
  });

  /// Recovered and structurally complete: one file, nothing trimmed.
  ///
  /// **Bucketed by the state each capture ended in, not by which function ran.**
  /// Before CHRN-114 every recovered capture was reported under `salvaged`,
  /// which made a field named for a trim list captures no trim had touched —
  /// the same lie as the chip, one level down.
  final List<String> ready;

  /// Recovered and genuinely torn: `audio.trimmed` written.
  final List<String> salvaged;

  /// Captures whose audio never reached the file. Kept, never sent.
  final List<String> empty;

  final List<String> stillRecording;

  /// Set when something could not be classified yet and deserves another look.
  final Duration? retryAfter;

  bool get isQuiet =>
      ready.isEmpty &&
      salvaged.isEmpty &&
      empty.isEmpty &&
      retryAfter == null;
}

/// Scans every capture under [root] and finishes the ones nobody is writing.
Future<RecoveryOutcome> recoverAll(
  Directory root,
  CaptureOwner owner, {
  DateTime? now,
}) async {
  final at = now ?? DateTime.now();
  final ready = <String>[];
  final salvaged = <String>[];
  final empty = <String>[];
  final live = <String>[];
  Duration? retryAfter;

  if (!await root.exists()) return const RecoveryOutcome();

  // Listed defensively. Recovery runs UNAWAITED at launch, so anything thrown
  // here lands in a zone with nobody to catch it -- and the directory really
  // can go away underneath the walk (an uninstall, the OS reclaiming storage,
  // a test tearing down). A capture we could not look at is one we look at next
  // time; it is never a reason to take the app down.
  final List<FileSystemEntity> entries;
  try {
    entries = await root.list().toList();
  } on FileSystemException {
    return const RecoveryOutcome();
  }

  for (final entry in entries) {
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

    // `recoveredAt` is what marks this as recovery's work rather than a stop
    // the app watched. It is the only durable trace that this capture was found
    // on disk instead of finished in front of somebody.
    final outcome = await classify(capture, record, recoveredAt: at);
    switch (outcome.state) {
      case CaptureState.ready:
        ready.add(captureId);
      case CaptureState.salvaged:
        salvaged.add(captureId);
      case CaptureState.empty:
        empty.add(captureId);
      case CaptureState.recording:
        // classify never returns this; listed so a new state cannot be added
        // without the compiler pointing here.
        live.add(captureId);
    }
  }

  return RecoveryOutcome(
    ready: ready,
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

/// Reads a capture's bytes, decides what it is, and writes the record.
///
/// **The one classifier, shared by both paths.** Before CHRN-114 the clean-stop
/// path carried this branch inline while recovery called the trim
/// unconditionally — which is how a whole file recovered after a crash came to
/// be written out a second time and labelled as though a trim had shortened it.
/// One function means the two paths cannot drift again.
///
/// [recoveredAt] is the only thing that differs between them: set when this
/// capture was found on disk, null when the app watched it stop. It changes no
/// branch here — it is recorded, not acted on.
Future<CaptureRecord> classify(
  CaptureDir capture,
  CaptureRecord record, {
  DateTime? recoveredAt,
}) async {
  final bytes = await capture.audio.exists()
      ? Uint8List.fromList(await capture.audio.readAsBytes())
      : Uint8List(0);
  final scan = scanOgg(bytes);

  // Nothing readable at all. Somebody spoke and none of it left the encoder.
  // The remnant stays, and carries no hash, so nothing can try to send it.
  if (scan.isEmpty) {
    return _write(
      capture,
      record.copyWith(
        state: CaptureState.empty,
        byteSize: bytes.length,
        trimOffset: scan.trimOffset,
        recoveredAt: recoveredAt,
      ),
    );
  }

  // Torn: the last complete page does not end at EOF, so the server would
  // refuse it a duration. The valid prefix goes to a NEW file and `audio.opus`
  // keeps every byte, including the ones past the cut.
  if (!scan.endsAtEof) {
    final keep = Uint8List.sublistView(bytes, 0, scan.trimOffset);
    await capture.trimmed.writeAsBytes(keep, flush: true);
    return _write(
      capture,
      record.copyWith(
        state: CaptureState.salvaged,
        contentHash: sha256.convert(keep).toString(),
        byteSize: keep.length,
        durationMs: scan.durationMs,
        trimOffset: scan.trimOffset,
        recoveredAt: recoveredAt,
      ),
    );
  }

  // Structurally complete. No second file: there is nothing to trim, and on
  // this platform this is the ORDINARY outcome of a kill, because the media
  // server finalises the file when the client dies. Writing a byte-for-byte
  // duplicate here is what CHRN-114 exists to stop.
  return _write(
    capture,
    record.copyWith(
      state: CaptureState.ready,
      contentHash: sha256.convert(bytes).toString(),
      byteSize: bytes.length,
      durationMs: scan.durationMs,
      recoveredAt: recoveredAt,
    ),
  );
}

Future<CaptureRecord> _write(CaptureDir capture, CaptureRecord record) async {
  await capture.writeMeta(record);
  return record;
}

/// Finishes a capture whose stop the app watched.
///
/// A thin call through to [classify] with no `recoveredAt`, which is the whole
/// of the difference. It still goes through the page scan rather than trusting
/// the file: the hash the server checks has to cover exactly the bytes that
/// will be sent, and a recorder that failed at `stop()` can leave a file that
/// looks finished and is not.
Future<CaptureRecord> finalise(CaptureDir capture, CaptureRecord record) =>
    classify(capture, record);
