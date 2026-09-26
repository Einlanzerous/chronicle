/// The gate: the ONE condition under which this device deletes a capture's
/// audio. CHRN-120, ruling 0 -- key on the server's own prune.
///
/// **The rule.** The phone deletes its copy only after the server has, for its
/// own reasons, deleted ITS copy: `GET /audio/{memo_id}` answering `410` with
/// code `audio_pruned`. That answer is the server's whole prune predicate
/// evaluated by the server -- `retention <> 'forever'`, the 30-day window or
/// `discard_now`, AND a durable transcript (`store.prunableClause`) -- and
/// `audio_pruned_at` is only ever set by a compare-and-swap over all of it
/// (`store.MarkAudioPruned`). So this file holds no copy of any of that: no
/// model floor, no window, no retention rule, nothing that can drift out of
/// step with `internal/store/retention.go` and start deleting audio the server
/// would still keep. CLAUDE.md invariant 1 -- audio is never pruned for a memo
/// whose transcription never succeeded -- is enforced by the server's
/// predicate, once, and this only ever agrees with it.
///
/// **A `410` is the only thing that permits a delete.** Everything else is a
/// refusal, and a refusal changes nothing on disk. In particular, no timer, no
/// local retention value and no local "acknowledged long enough ago" ever
/// permits one -- `prune.dart`'s poll floor only decides when to ASK.
///
/// **The response table** is here, pure and tested row by row
/// (`test/queue/prune_gate_test.dart`), because the generated client's own
/// habit -- throw on any status >= 400 -- would have made the positive signal
/// unhandleable, and a blanket "any non-2xx is a skip" would have made it
/// unreachable.
library;

import 'dart:convert';

import 'audio_gate_transport.dart';
import 'failure.dart';

/// What the gate concluded from one probe.
sealed class PruneVerdict {
  const PruneVerdict();
}

/// The server has pruned its own copy: this device's may go.
class PruneEligible extends PruneVerdict {
  const PruneEligible();

  /// Written into the tombstone as the gate's own outcome code.
  static const code = 'audio_pruned';
}

/// Do NOT delete. [reason] is a short machine token for the log line and the
/// tests; it is never parsed back into behaviour.
///
/// [warn] marks the answers that may mean this device now holds the ONLY copy
/// -- the server's row or file has lost its audio outside the ordinary prune
/// path -- which deserve a louder line than "not yet".
///
/// [endsPass] marks a server that is refusing to answer well (busy, erroring):
/// nothing else asked this pass will fare better, and hammering it is worse.
class PruneKeep extends PruneVerdict {
  const PruneKeep(this.reason, {this.warn = false, this.endsPass = false});

  final String reason;
  final bool warn;
  final bool endsPass;
}

/// No usable answer about THIS memo: the pass ends, and the capture is left
/// exactly as it was -- not even `lastPolledAt` moves, because nothing was
/// learned. Carries `failure.dart`'s own classification rather than a second
/// one.
class PruneEnds extends PruneVerdict {
  const PruneEnds(this.reason);

  final PassEndReason reason;
}

/// Reads one probe's answer. Pure: a status and a body in, a verdict out.
PruneVerdict classifyAudioProbe(AudioProbeResponse response) {
  final status = response.statusCode;
  final code = _errorCode(response.body);

  if (status == 401) return const PruneEnds(PassEndReason.signedOut);

  // The only delete signal, and it needs BOTH halves. A 410 from a proxy, or
  // from a server whose error envelope has changed, is a shape this code has
  // never seen and must not delete on.
  if (status == 410) {
    return code == PruneEligible.code
        ? const PruneEligible()
        : const PruneKeep('unrecognised_410', warn: true);
  }

  // A ranged request honoured: the audio is still there.
  if (status == 206) return const PruneKeep('not_yet_pruned');
  // `Range` ignored (a proxy, or a server that does not honour it): the audio
  // is still there, and this probe just downloaded all of it.
  if (status == 200) return const PruneKeep('range_not_honoured');

  // The row expects audio the file no longer has, or the memo is not readable
  // by this account. Either way the server may have lost its copy outside the
  // ordinary prune, which makes this device's the last one.
  if (status == 500 && code == 'audio_missing') {
    return const PruneKeep('audio_missing', warn: true);
  }
  if (status == 404) return const PruneKeep('not_found', warn: true);

  if (status == 429 || status == 503) {
    return PruneKeep('throttled_$status', endsPass: true);
  }
  if (status >= 500 && status < 600) {
    return PruneKeep('server_error_$status', endsPass: true);
  }
  if (status == 400) return const PruneKeep('bad_request');

  // Any other 2xx, 3xx or 4xx is not a shape this gate knows. Never delete on
  // it, and say so.
  return PruneKeep('unexpected_$status', warn: true);
}

/// Reads a probe that threw. Reuses `classifyError` for exactly what it is
/// calibrated for here -- no answer at all, and a host that is not Chronicle --
/// and for nothing else: its 4xx vocabulary is the SEND path's, and a probe
/// never throws a status.
PruneEnds classifyProbeFailure(Object error) {
  final outcome = classifyError(error);
  if (outcome is PassEnds) return PruneEnds(outcome.reason);
  // A shape `classifyError` reads as a per-capture outcome cannot arise from
  // a transport that returns statuses rather than throwing them. If one does
  // anyway, "no answer" is the safe reading: end the pass, delete nothing.
  return const PruneEnds(PassEndReason.network);
}

String? _errorCode(String? body) {
  if (body == null || body.isEmpty) return null;
  try {
    final decoded = jsonDecode(body);
    if (decoded is Map) return decoded['code'] as String?;
  } catch (_) {
    // A non-JSON body carries no code, which is what the callers treat it as.
  }
  return null;
}
