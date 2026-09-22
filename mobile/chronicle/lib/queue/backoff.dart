/// Per-capture retry backoff: 15 seconds, doubling, capped at 15 minutes,
/// jittered by capture id so captures enqueued together do not all retry in
/// the same instant.
///
/// **Clearing backoff never writes anything.** Network regained, app
/// resume, a new capture reaching `ready`, and a manual "Try again" all want
/// to make a waiting capture eligible again without disturbing
/// `lastAttemptAt` -- rewriting it would make the queue screen's own
/// last-attempt line ("server answered 500 at 14:02") lie about when the
/// server was actually last asked. So clearing is an in-memory override the
/// caller holds (`clearedAfter`) and compares against the persisted
/// `lastAttemptAt` at read time; nothing here is ever persisted.
library;

const Duration _initial = Duration(seconds: 15);
const Duration _cap = Duration(minutes: 15);

/// The delay before a capture on its [attemptCount]th attempt should be
/// retried, before jitter. Zero for a capture that has never been tried.
Duration backoffDelay(int attemptCount) {
  if (attemptCount <= 0) return Duration.zero;
  final doublings = (attemptCount - 1).clamp(0, 20);
  final scaled = _initial * (1 << doublings);
  return scaled > _cap ? _cap : scaled;
}

/// Deterministic per-capture jitter in `[0, delay/4)`, so two captures that
/// share an attempt count do not retry in lockstep. Derived from the
/// capture id rather than `Random`, so it is stable across a single
/// capture's own retries and predictable in a test.
Duration _jitter(String captureId, Duration delay) {
  final quarterMs = delay.inMilliseconds ~/ 4;
  if (quarterMs <= 0) return Duration.zero;
  final h = captureId.codeUnits
      .fold<int>(0, (acc, c) => (acc * 31 + c) & 0x7fffffff);
  return Duration(milliseconds: h % quarterMs);
}

/// Whether [captureId] is eligible to retry at [now], given its persisted
/// [attemptCount] and [lastAttemptAt], and an optional in-memory
/// [clearedAfter] override (see the library doc).
///
/// A capture that has never been attempted ([lastAttemptAt] null) is always
/// eligible: backoff bounds RETRIES, never the first attempt.
bool backoffElapsed({
  required String captureId,
  required int attemptCount,
  required DateTime? lastAttemptAt,
  required DateTime now,
  DateTime? clearedAfter,
}) {
  if (lastAttemptAt == null) return true;
  if (clearedAfter != null && !clearedAfter.isBefore(lastAttemptAt)) {
    return true;
  }
  final delay = backoffDelay(attemptCount);
  final wait = delay + _jitter(captureId, delay);
  return now.isAfter(lastAttemptAt.add(wait));
}
