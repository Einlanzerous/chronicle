import 'package:chronicle/queue/retention_gate.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('the shipped grace is zero -- CHRN-61 ruling 3, pending CHRN-62', () {
    expect(retentionGrace, Duration.zero);
  });

  group('retentionGateOpen against the shipped (zero) grace', () {
    final enqueuedAt = DateTime(2026, 1, 1, 12);

    test('a declared retention always opens the gate immediately', () {
      expect(
        retentionGateOpen(retention: 'days_30', enqueuedAt: enqueuedAt, now: enqueuedAt),
        isTrue,
      );
    });

    test('with the shipped grace, an undeclared capture opens at once too', () {
      expect(
        retentionGateOpen(retention: null, enqueuedAt: enqueuedAt, now: enqueuedAt),
        isTrue,
      );
    });
  });

  group('retentionGateOpen against a supplied grace -- the formula CHRN-62 inherits', () {
    final enqueuedAt = DateTime(2026, 1, 1, 12);
    const grace = Duration(hours: 24);

    test('a declared retention still opens immediately, grace or no grace', () {
      expect(
        retentionGateOpen(
          retention: 'forever',
          enqueuedAt: enqueuedAt,
          now: enqueuedAt,
          grace: grace,
        ),
        isTrue,
      );
    });

    test('an undeclared capture is held before the grace elapses', () {
      expect(
        retentionGateOpen(
          retention: null,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt.add(const Duration(hours: 23)),
          grace: grace,
        ),
        isFalse,
      );
    });

    test('exactly at the grace, the gate opens', () {
      expect(
        retentionGateOpen(
          retention: null,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt.add(grace),
          grace: grace,
        ),
        isTrue,
      );
    });

    test('past the grace, the gate stays open', () {
      expect(
        retentionGateOpen(
          retention: null,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt.add(const Duration(hours: 25)),
          grace: grace,
        ),
        isTrue,
      );
    });

    test('a kill between ready and enqueue can only extend the window, never shorten it: '
        'an enqueuedAt moved LATER than "now" never opens early', () {
      expect(
        retentionGateOpen(
          retention: null,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt.subtract(const Duration(minutes: 1)),
          grace: grace,
        ),
        isFalse,
      );
    });
  });
}
