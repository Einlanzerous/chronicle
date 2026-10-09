/// The retention gate: CHRN-60's ruling 3, "hold the declaration until
/// retention is known, with a grace" -- CHRN-61 built the read side and
/// shipped the grace at zero (its ruling 3), and CHRN-62 restored it when it
/// added the confirm that writes the choice.
///
/// Retention is ratchet-only on the server (`store.Arrival`'s ratchet only
/// ever raises it), so a memo declared with the deployment default can
/// never afterwards be lowered to `discard_now`. This gate decides WHEN a
/// capture may be declared, never what retention to declare it with beyond
/// "whatever `meta.json` already says, or nothing".
///
/// A capture is in exactly one of three places:
///
/// * **decided** -- `retention` is set. Declared at once, carrying it.
/// * **skipped** -- the person saw the confirm and chose not to choose
///   (`CaptureRecord.retentionSkippedAt`). Declared at once, with no
///   opinion, so the deployment default applies.
/// * **not yet decided** -- neither. Held until [retentionGrace] has passed
///   since the queue first saw it, then declared with no opinion.
///
/// A capture whose queue record already shows a send attempt
/// (`queueShowsAttempt`) is never held (CHRN-127): it predates the confirm,
/// or has already been offered to the server, so the person can never be
/// offered a choice and holding it would only delay it. It sends at the
/// default.
library;

/// How long a capture nobody has decided on is held before it is declared
/// at the deployment default.
///
/// **CHRN-62 set this to 24 hours**, in the same change that added the
/// confirm, as CHRN-61's approved plan (ruling 3) and CHRN-62's Done-when
/// require. At zero -- its value while no confirm existed -- a capture was
/// declared the instant it was `ready`, so DISCARD NOW would have been
/// offered on a memo the server could never be told to lower.
const Duration retentionGrace = Duration(hours: 24);

/// How long before the grace ends the confirm stops accepting a choice.
///
/// The engine reads `meta.json` at the start of a pass and declares from
/// that copy. A choice written in the last moment of the grace could land
/// after a pass has already read "no opinion, grace over" and opened the
/// upload with nothing -- the server keeps the first declaration, so the
/// choice would be shown as made and never honoured. Closing the choice a
/// little early means the two can never overlap.
const Duration retentionChoiceCloses = Duration(minutes: 5);

/// When the grace for a capture first seen sendable at [enqueuedAt] ends.
DateTime retentionGraceEndsAt(DateTime enqueuedAt, {Duration grace = retentionGrace}) =>
    enqueuedAt.add(grace);

/// Whether a capture may be declared to the server right now.
///
/// [attempted] is `queueShowsAttempt` for the capture's queue record; an
/// attempted capture is never held.
///
/// [retention] is `meta.json`'s value: null means "no opinion yet". [skipped]
/// is whether `meta.json` carries CHRN-62's skip marker. [now] minus
/// [enqueuedAt] is measured against [grace], which defaults to the module
/// constant above. The engine never passes [grace] explicitly --
/// [retentionGrace] is the one place production behaviour can change; the
/// parameter exists only so a test can exercise the formula against another
/// grace without waiting on the real one.
bool retentionGateOpen({
  required String? retention,
  required bool skipped,
  required DateTime enqueuedAt,
  required DateTime now,
  bool attempted = false,
  Duration grace = retentionGrace,
}) {
  if (retention != null || skipped || attempted) return true;
  return !now.isBefore(retentionGraceEndsAt(enqueuedAt, grace: grace));
}

/// Whether the person may still make (or skip) the choice for a capture.
///
/// Only while the gate is closed, and not within [retentionChoiceCloses] of
/// it opening on its own. Once anything has been declared the choice is
/// fixed: the server keeps a session's first declaration, and a completed
/// memo's retention can only be raised by a later arrival, never lowered.
bool retentionChoiceOpen({
  required String? retention,
  required bool skipped,
  required DateTime enqueuedAt,
  required DateTime now,
  Duration grace = retentionGrace,
}) {
  if (retention != null || skipped) return false;
  final closes = retentionGraceEndsAt(enqueuedAt, grace: grace).subtract(retentionChoiceCloses);
  return now.isBefore(closes);
}
