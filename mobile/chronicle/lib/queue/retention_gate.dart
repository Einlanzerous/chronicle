/// The retention gate: CHRN-60's ruling 3, "hold the declaration until
/// retention is known, with a grace" -- and CHRN-61's own ruling 3, which
/// picked shipping that grace at zero until CHRN-62 exists to set it.
///
/// Retention is ratchet-only on the server (`store.Arrival`'s ratchet only
/// ever raises it), so a memo declared with the deployment default can
/// never afterwards be lowered to `discard_now`. This gate is the READ side
/// of the hold CHRN-62 writes: it decides WHEN a capture may be declared,
/// never what retention to declare it with beyond "whatever `meta.json`
/// already says, or nothing".
library;

/// **CHRN-62 must set this to 24 hours in the PR that adds the confirm
/// screen**, and must write the "skipped versus not yet seen" marker this
/// gate would then need to read to tell the two apart. Left at zero, a
/// capture with no retention opinion is declared at the server default the
/// instant it is `ready` -- correct while CHRN-62 does not exist, and wrong
/// the day it does and forgets to flip this: DISCARD NOW would then be
/// offered on a memo the server can never be told to lower. See CHRN-61's
/// approved plan (ruling 3) and CHRN-62's Done-when.
const Duration retentionGrace = Duration.zero;

/// Whether a capture may be declared to the server right now.
///
/// [retention] is `meta.json`'s value: null means "no opinion yet". [now]
/// minus [enqueuedAt] is measured against [grace], which defaults to the
/// module constant above. The engine never passes [grace] explicitly --
/// [retentionGrace] is the one place production behaviour can change; the
/// parameter exists only so a test can exercise the formula against a
/// non-zero grace without waiting on the real one.
bool retentionGateOpen({
  required String? retention,
  required DateTime enqueuedAt,
  required DateTime now,
  Duration grace = retentionGrace,
}) {
  if (retention != null) return true;
  return !now.isBefore(enqueuedAt.add(grace));
}
