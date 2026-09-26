/// The prune pass: CHRN-120's one deletion path over the phone's copy of a
/// memo's audio. CHRN-61's ruling 4 shipped none; this is the follow-up it
/// promised, and it is the client's own prune of its own LOCAL copy -- distinct
/// from the server's (CHRN-22), and gated on it.
///
/// **What it deletes, and when.** The sendable file of a capture whose
/// `QueueRecord.status` is `acknowledged`, and only after `prune_gate.dart`
/// reports that the SERVER has pruned its own copy (`GET /audio/{id}` -> `410
/// audio_pruned`). Nothing here decides that a delete is safe -- not a timer,
/// not a local retention value, not "acknowledged long enough ago". The floor
/// below decides only when to ASK.
///
/// **The order, and why each step is where it is.** For a capture the gate
/// permits:
///
/// 1. the `pruned` tombstone is written (`CaptureDir.writePruneTombstone`);
/// 2. `locallyPrunedAt` is written to `upload.json`;
/// 3. the sendable file is deleted (`CaptureDir.deleteSendable`).
///
/// Never after, and never out of order: a crash at any point leaves at most a
/// tombstone and/or a mark with the file still present, and the next pass
/// finishes the unlink WITHOUT asking the server again -- a marked capture is
/// never re-polled. What a finishing pass will not do is delete a file that has
/// no tombstone.
///
/// **What it never touches.** `meta.json`, `upload.json`, the tombstone, and --
/// on a salvage -- the untrimmed `audio.opus`, whose tail past the trim cut the
/// server never received. Only the file the queue actually sent is ever
/// eligible (`CaptureDir.sendable`).
///
/// **It ships dark.** [pruneLocalAudioEnabled] is a compile-time constant that
/// defaults to false, so a build that does not opt in never asks the server
/// and never deletes anything. See its own comment for why.
library;

import 'dart:async';
import 'dart:io' show FileSystemException;

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:flutter_riverpod/flutter_riverpod.dart' show Provider;

import '../capture/capture_record.dart';
import 'audio_gate_transport.dart';
import 'device_block.dart';
import 'engine.dart' show QueueCapture;
import 'failure.dart' show PassEndReason;
import 'prune_gate.dart';
import 'queue_record.dart';

/// Whether this build deletes local audio at all. **Off unless a build opts
/// in**, with `--dart-define=CHRONICLE_PRUNE_LOCAL_AUDIO=true`.
///
/// CHRN-120's backup ruling: the shared Postgres has no backup (CHRN-68,
/// SERV-43), so once this device deletes its copy a lost database loses the
/// memo outright, where today it would only cost derived data. Until that gap
/// is closed this ships default-off: the code lands and is reviewed and tested,
/// what is deferred is storage reclamation.
///
/// **This is a default, not a lock.** Chronicle has no APK release track yet
/// and so no guard against a release build passing this define (Lyceum's
/// `tool/check_store_build.sh` rejects any `--dart-define`; the ticket that
/// adds Chronicle's release track owns the equivalent, per `server_url.dart`).
/// `test/queue/prune_test.dart` pins the default to false so a change to it
/// fails CI, and the follow-up that flips it is the one that closes the backup
/// gap.
const bool pruneLocalAudioEnabled =
    bool.fromEnvironment('CHRONICLE_PRUNE_LOCAL_AUDIO');

/// Whether the foreground prunes at all. Exactly [pruneLocalAudioEnabled] --
/// the compile-time constant, false unless a build opts in -- and a provider
/// only so a test can turn it on without a `--dart-define`. Nothing in `lib/`
/// overrides it.
///
/// **It lives here, and not beside `QueueController`, on purpose.** This file is
/// one `pr-review.yml`'s `sensitive_paths` names, so a change to the value that
/// decides whether deletion happens at all is reviewed at the expensive tier.
/// In `queue_controller.dart` a one-line edit turning it on in every build would
/// reverse the backup ruling and go through the cheap one.
final pruneEnabledProvider = Provider<bool>((ref) => pruneLocalAudioEnabled);

/// At most this many captures are asked about in one pass. A bound on the work
/// one wake can do, not on the rate -- [minPollInterval] is the rate.
const int maxProbesPerPass = 10;

/// No capture is asked about more than once in this long. Without it every
/// acknowledged capture would be probed on every wake -- roughly 10^4 requests
/// a device a day -- and a memo whose audio is already gone with no audio row
/// would write a server-side ERROR line each time (`api/memos.go`'s
/// `audio_missing`).
const Duration minPollInterval = Duration(hours: 24);

/// The server's prune window, as a SCHEDULING HINT only. It is a compile-time
/// constant server-side (`audio.ProjectionWindow`), not configurable, and this
/// copy is drift-safe both ways: a shorter server window only delays the
/// phone's prune, a longer one costs a daily probe until the `410` arrives. It
/// never permits a delete -- only `prune_gate.dart` does.
const Duration serverAudioWindowHint = Duration(days: 30);

/// The earliest moment the server could possibly answer `410` for this
/// capture, or null when it never will and there is nothing to ask.
///
/// * `forever` -- the server never prunes it. Skipping the probe is an
///   optimisation and nothing more: a local value can only ever save a
///   request, never permit a delete (`prune_gate.dart`).
/// * `discard_now` -- eligible as soon as it is transcribed, so from the ack.
/// * anything else (`days_30`, or no opinion) -- the server's window from
///   arrival, which never precedes `startedAt`, so `startedAt` + the window can
///   only ask early, never late.
DateTime? pollFloor(CaptureRecord capture, QueueRecord record) {
  switch (capture.retention) {
    case 'forever':
      return null;
    case 'discard_now':
      return record.acknowledgedAt;
    default:
      return capture.startedAt.add(serverAudioWindowHint);
  }
}

/// When this capture may next be asked about: the floor, pushed out to
/// [minPollInterval] after the last time it was.
DateTime? nextPollAt(CaptureRecord capture, QueueRecord record) {
  final floor = pollFloor(capture, record);
  if (floor == null) return null;
  final last = record.lastPolledAt;
  if (last == null) return floor;
  final earliestAgain = last.add(minPollInterval);
  return earliestAgain.isAfter(floor) ? earliestAgain : floor;
}

/// What one pass did. For tests and for the log; nothing reads it back into
/// behaviour.
class PruneReport {
  /// Captures the server was asked about.
  int probed = 0;

  /// Captures whose audio this pass deleted.
  int pruned = 0;

  /// Captures a crashed earlier pass had marked, whose unlink this pass
  /// finished.
  int finished = 0;

  /// Why the pass stopped early, if it did.
  PassEndReason? endedBy;
}

/// One pass over [captures], run immediately after `drainPass` on every trigger
/// the queue wakes on -- it never precedes a send.
///
/// Reads every capture's `upload.json` FRESH rather than trusting
/// [QueueCapture.queueRecord], which is a snapshot from before the drain and
/// may predate an ack.
///
/// Does nothing under an active [deviceBlock] (the send path's own arbitration
/// is the only writer of that state -- this pass ends on a 401 or a wrong host
/// but never raises one), and does nothing at all when [enabled] is false.
/// Stops on the first failure to get any answer, the way `drainPass` does.
///
/// Never throws for one capture's sake: a failure preparing or finishing a
/// prune is logged and that capture is left as the ordering above guarantees it
/// can be found again.
Future<PruneReport> prunePass({
  required List<QueueCapture> captures,
  required AudioGateTransport transport,
  DeviceBlock? deviceBlock,
  DateTime Function()? now,
  bool? enabled,
  int maxProbes = maxProbesPerPass,
  void Function(String message)? log,
}) async {
  final report = PruneReport();
  if (!(enabled ?? pruneLocalAudioEnabled)) return report;
  if (deviceBlock != null) return report;

  final say = log ?? debugPrint;
  final clock = now ?? DateTime.now;
  final at = clock();

  final due = <_Candidate>[];
  for (final qc in captures) {
    try {
      final candidate = await _inspect(qc, at, report, say);
      if (candidate != null) due.add(candidate);
    } catch (e) {
      say('chronicle: prune could not inspect ${qc.capture.captureId}: $e');
    }
  }

  // Least recently asked first, so an unresolvable capture at the head of the
  // queue (a `forever` memo, one whose transcription never finished) cannot
  // starve the ones behind it. Never asked sorts before everything asked.
  due.sort((a, b) {
    final byPolled = (a.record.lastPolledAt ?? _epoch)
        .compareTo(b.record.lastPolledAt ?? _epoch);
    if (byPolled != 0) return byPolled;
    return a.qc.capture.startedAt.compareTo(b.qc.capture.startedAt);
  });

  var loggedRangeIgnored = false;
  for (final c in due.take(maxProbes)) {
    final AudioProbeResponse response;
    try {
      response = await transport.probe(c.memoId);
    } catch (e) {
      // No answer at all: nothing was learned about this memo, so nothing is
      // written for it -- not even `lastPolledAt`, or a flaky network would
      // spend every capture's daily allowance without asking anything.
      report.endedBy = classifyProbeFailure(e).reason;
      break;
    }
    report.probed++;

    final verdict = classifyAudioProbe(response);
    if (verdict is PruneEnds) {
      report.endedBy = verdict.reason;
      break;
    }

    try {
      if (verdict is PruneEligible) {
        await _prune(c, clock(), say);
        report.pruned++;
        continue;
      }

      final keep = verdict as PruneKeep;
      await _recordPoll(c.qc.queueDir, clock());
      if (keep.reason == 'range_not_honoured') {
        if (!loggedRangeIgnored) {
          loggedRangeIgnored = true;
          say('chronicle: GET /audio ignored Range; each probe downloads the '
              'whole recording (this server or a proxy in front of it)');
        }
      } else if (keep.warn) {
        say('chronicle: kept local audio for ${c.memoId}: ${keep.reason} '
            '(this device may hold the only copy)');
      }
      if (keep.endsPass) break;
    } catch (e) {
      say('chronicle: prune failed for ${c.memoId}: $e');
    }
  }
  return report;
}

final _epoch = DateTime.fromMillisecondsSinceEpoch(0);

class _Candidate {
  _Candidate(this.qc, this.record, this.memoId, this.bytes);

  final QueueCapture qc;
  final QueueRecord record;
  final String memoId;
  final int bytes;
}

/// Decides what one capture needs from this pass. Returns a candidate to ask
/// the server about, or null -- having, on the way, finished the unlink of a
/// capture an earlier pass already decided to prune.
Future<_Candidate?> _inspect(
  QueueCapture qc,
  DateTime now,
  PruneReport report,
  void Function(String) say,
) async {
  final record = await qc.queueDir.read();
  if (record == null || record.status != QueueStatus.acknowledged) return null;

  final captureDir = qc.queueDir.capture;
  final state = qc.capture.state;
  if (state != CaptureState.ready && state != CaptureState.salvaged) return null;

  final tombstone = await captureDir.readPruneTombstone();
  if (record.locallyPrunedAt != null || tombstone != null) {
    // Already decided. Finish it; never ask the server again.
    if (await _finish(qc, record, tombstone, say)) report.finished++;
    return null;
  }

  final memoId = record.memoId;
  if (memoId == null || memoId.isEmpty) return null;

  final next = nextPollAt(qc.capture, record);
  if (next == null || now.isBefore(next)) return null;

  // The file must be there and be the file that was acknowledged. Asking the
  // server about a capture with nothing left to delete, or deleting bytes that
  // are no longer the ones the server verified, would each be a mistake.
  final file = captureDir.sendable(state);
  final int length;
  try {
    length = await file.length();
  } on FileSystemException {
    return null;
  }
  if (length != qc.capture.byteSize) return null;

  return _Candidate(qc, record, memoId, length);
}

/// The whole deletion, in its fixed order. Called only for a `410
/// audio_pruned`.
Future<void> _prune(_Candidate c, DateTime now, void Function(String) say) async {
  final captureDir = c.qc.queueDir.capture;

  await captureDir.writePruneTombstone(PruneTombstone(
    memoId: c.memoId,
    prunedAt: now,
    prunedBytes: c.bytes,
    gate: PruneEligible.code,
  ));
  await _writeMark(c.qc.queueDir, prunedAt: now, polledAt: now);
  await captureDir.deleteSendable(c.qc.capture.state);

  say('chronicle: pruned local audio memo=${c.memoId} '
      'gate=${PruneEligible.code} bytes=${c.bytes}');
}

/// Completes a prune an earlier pass began. Returns whether it deleted
/// anything.
///
/// Never deletes without a tombstone: a mark with no tombstone is a state the
/// ordering cannot produce, and the safe answer to a state that should not
/// exist is to leave the file where it is.
Future<bool> _finish(
  QueueCapture qc,
  QueueRecord record,
  PruneTombstone? tombstone,
  void Function(String) say,
) async {
  if (tombstone == null) {
    say('chronicle: ${qc.capture.captureId} is marked pruned but has no '
        'tombstone; leaving its audio alone');
    return false;
  }
  if (record.locallyPrunedAt == null) {
    await _writeMark(qc.queueDir,
        prunedAt: tombstone.prunedAt, polledAt: tombstone.prunedAt);
  }
  final file = qc.queueDir.capture.sendable(qc.capture.state);
  if (!await file.exists()) return false;
  await qc.queueDir.capture.deleteSendable(qc.capture.state);
  say('chronicle: finished pruning local audio memo=${tombstone.memoId} '
      'gate=${tombstone.gate}');
  return true;
}

/// Writes `locallyPrunedAt` (and `lastPolledAt`) onto whatever `upload.json`
/// says NOW, refusing to write over anything that is not `acknowledged`.
Future<void> _writeMark(
  QueueDir dir, {
  required DateTime prunedAt,
  required DateTime polledAt,
}) async {
  final onDisk = await dir.read();
  if (onDisk == null || onDisk.status != QueueStatus.acknowledged) {
    throw StateError('refusing to mark a capture pruned when its record is '
        'not acknowledged');
  }
  await dir.write(onDisk.copyWith(locallyPrunedAt: prunedAt, lastPolledAt: polledAt));
}

/// A refusal's only effect: `lastPolledAt`. Read fresh, and never written over
/// a record that is not `acknowledged`.
Future<void> _recordPoll(QueueDir dir, DateTime polledAt) async {
  final onDisk = await dir.read();
  if (onDisk == null || onDisk.status != QueueStatus.acknowledged) return;
  await dir.write(onDisk.copyWith(lastPolledAt: polledAt));
}
