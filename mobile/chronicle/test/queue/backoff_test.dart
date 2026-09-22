import 'package:chronicle/queue/backoff.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('backoffDelay', () {
    test('a capture never tried has no delay', () {
      expect(backoffDelay(0), Duration.zero);
    });

    test('doubles from 15 seconds', () {
      expect(backoffDelay(1), const Duration(seconds: 15));
      expect(backoffDelay(2), const Duration(seconds: 30));
      expect(backoffDelay(3), const Duration(seconds: 60));
      expect(backoffDelay(4), const Duration(seconds: 120));
    });

    test('caps at 15 minutes and never exceeds it, however high the count', () {
      expect(backoffDelay(20), const Duration(minutes: 15));
      expect(backoffDelay(1000), const Duration(minutes: 15));
    });
  });

  group('backoffElapsed', () {
    final now = DateTime(2026, 1, 1, 12);

    test('a capture with no lastAttemptAt is always eligible', () {
      expect(
        backoffElapsed(
          captureId: 'a',
          attemptCount: 5,
          lastAttemptAt: null,
          now: now,
        ),
        isTrue,
      );
    });

    test('not yet eligible just after the first failed attempt', () {
      expect(
        backoffElapsed(
          captureId: 'a',
          attemptCount: 1,
          lastAttemptAt: now,
          now: now.add(const Duration(seconds: 1)),
        ),
        isFalse,
      );
    });

    test('eligible once the full delay plus jitter has passed', () {
      expect(
        backoffElapsed(
          captureId: 'a',
          attemptCount: 1,
          lastAttemptAt: now,
          // 15s delay; jitter is at most delay/4 (3.75s), so 20s is safely past it.
          now: now.add(const Duration(seconds: 20)),
        ),
        isTrue,
      );
    });

    test('captures with the same attempt count do not all share a retry instant', () {
      // Deterministic, not a coin flip: jitter is derived from the capture
      // id, so the exact millisecond each becomes eligible ("crossover") is
      // reproducible. A handful of distinct ids should not all cross over
      // at the same millisecond -- if they did, the jitter would not be
      // doing the one job it exists for.
      final delay = backoffDelay(3);
      final quarterMs = delay.inMilliseconds ~/ 4;

      int crossoverMs(String id) {
        for (var ms = 0; ms <= quarterMs; ms++) {
          final eligible = backoffElapsed(
            captureId: id,
            attemptCount: 3,
            lastAttemptAt: now,
            now: now.add(delay + Duration(milliseconds: ms)),
          );
          if (eligible) return ms;
        }
        return quarterMs;
      }

      final crossovers = {
        for (final id in ['capture-a', 'capture-b', 'capture-c', 'capture-d', 'capture-e'])
          id: crossoverMs(id),
      };
      expect(
        crossovers.values.toSet().length,
        greaterThan(1),
        reason: 'jitter should not be identical across distinct capture ids: $crossovers',
      );

      // And whatever the jitter, everyone is eligible well past the cap.
      expect(
        backoffElapsed(
          captureId: 'capture-a',
          attemptCount: 3,
          lastAttemptAt: now,
          now: now.add(delay + const Duration(minutes: 1)),
        ),
        isTrue,
      );
    });

    test('clearedAfter makes a waiting capture eligible without any write', () {
      final lastAttemptAt = now;
      final stillWaiting = backoffElapsed(
        captureId: 'a',
        attemptCount: 4, // several minutes of backoff
        lastAttemptAt: lastAttemptAt,
        now: now.add(const Duration(seconds: 5)),
      );
      expect(stillWaiting, isFalse);

      final clearedAndEligible = backoffElapsed(
        captureId: 'a',
        attemptCount: 4,
        lastAttemptAt: lastAttemptAt,
        now: now.add(const Duration(seconds: 5)),
        clearedAfter: now.add(const Duration(seconds: 2)),
      );
      expect(clearedAndEligible, isTrue);
    });

    test('a clearedAfter that predates the last attempt does not force eligibility', () {
      expect(
        backoffElapsed(
          captureId: 'a',
          attemptCount: 4,
          lastAttemptAt: now,
          now: now.add(const Duration(seconds: 5)),
          clearedAfter: now.subtract(const Duration(minutes: 1)),
        ),
        isFalse,
      );
    });
  });
}
