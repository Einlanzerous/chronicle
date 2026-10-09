import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:chronicle/queue/retention_gate.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('the shipped grace is 24 hours -- CHRN-62, restoring CHRN-61 ruling 3\'s hold', () {
    // Pinned because it is the only safeguard between a confirm screen and a
    // server that can never lower retention: at zero, a capture is declared
    // at the default before anybody can tap DISCARD NOW. Changing it means
    // changing this test, deliberately.
    expect(retentionGrace, const Duration(hours: 24));
  });

  final enqueuedAt = DateTime(2026, 1, 1, 12);

  group('retentionGateOpen against the shipped grace', () {
    test('a declared retention always opens the gate immediately', () {
      expect(
        retentionGateOpen(
            retention: 'days_30', skipped: false, enqueuedAt: enqueuedAt, now: enqueuedAt),
        isTrue,
      );
    });

    test('a skipped capture opens the gate immediately, with no opinion', () {
      expect(
        retentionGateOpen(retention: null, skipped: true, enqueuedAt: enqueuedAt, now: enqueuedAt),
        isTrue,
      );
    });

    test('an undecided capture is NOT declared the moment it is ready', () {
      expect(
        retentionGateOpen(retention: null, skipped: false, enqueuedAt: enqueuedAt, now: enqueuedAt),
        isFalse,
      );
    });

    test('an undecided capture is held until the grace ends, then opens', () {
      expect(
        retentionGateOpen(
          retention: null,
          skipped: false,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt.add(const Duration(hours: 23, minutes: 59)),
        ),
        isFalse,
      );
      expect(
        retentionGateOpen(
          retention: null,
          skipped: false,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt.add(const Duration(hours: 24)),
        ),
        isTrue,
      );
    });
  });

  group('retentionGateOpen against a supplied grace -- the formula itself', () {
    const grace = Duration(hours: 2);

    test('a declared retention still opens immediately, grace or no grace', () {
      expect(
        retentionGateOpen(
          retention: 'forever',
          skipped: false,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt,
          grace: grace,
        ),
        isTrue,
      );
    });

    test('exactly at the grace, the gate opens', () {
      expect(
        retentionGateOpen(
          retention: null,
          skipped: false,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt.add(grace),
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
          skipped: false,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt.subtract(const Duration(minutes: 1)),
          grace: grace,
        ),
        isFalse,
      );
    });
  });

  group('retentionChoiceOpen -- when the confirm may still write', () {
    test('open for an undecided capture inside its grace', () {
      expect(
        retentionChoiceOpen(retention: null, skipped: false, enqueuedAt: enqueuedAt, now: enqueuedAt),
        isTrue,
      );
    });

    test('closed once a choice exists -- the choice is made once', () {
      expect(
        retentionChoiceOpen(
            retention: 'discard_now', skipped: false, enqueuedAt: enqueuedAt, now: enqueuedAt),
        isFalse,
      );
    });

    test('closed once skipped -- a skip is a decision too', () {
      expect(
        retentionChoiceOpen(retention: null, skipped: true, enqueuedAt: enqueuedAt, now: enqueuedAt),
        isFalse,
      );
    });

    test('closes retentionChoiceCloses before the gate opens, so the two never overlap', () {
      final gateOpens = retentionGraceEndsAt(enqueuedAt);
      final justInside = gateOpens.subtract(retentionChoiceCloses).subtract(const Duration(seconds: 1));
      final atClose = gateOpens.subtract(retentionChoiceCloses);
      expect(
        retentionChoiceOpen(retention: null, skipped: false, enqueuedAt: enqueuedAt, now: justInside),
        isTrue,
      );
      expect(
        retentionChoiceOpen(retention: null, skipped: false, enqueuedAt: enqueuedAt, now: atClose),
        isFalse,
      );
      // And at no instant are both "the confirm may write" and "the queue
      // may declare with no opinion" true.
      for (var m = 0; m <= 24 * 60; m += 1) {
        final now = enqueuedAt.add(Duration(minutes: m));
        final choice =
            retentionChoiceOpen(retention: null, skipped: false, enqueuedAt: enqueuedAt, now: now);
        final gate =
            retentionGateOpen(retention: null, skipped: false, enqueuedAt: enqueuedAt, now: now);
        expect(choice && gate, isFalse, reason: 'overlap at +${m}m');
      }
    });
  });

  group('CHRN-127 -- attempted captures', () {
    test('an attempted undecided capture opens the gate at once', () {
      expect(
        retentionGateOpen(
          retention: null,
          skipped: false,
          enqueuedAt: enqueuedAt,
          now: enqueuedAt,
          attempted: true,
        ),
        isTrue,
      );
    });

    test('queueShowsAttempt reads status, attemptCount and lastAttemptAt', () {
      QueueRecord r({QueueStatus s = QueueStatus.pending, int n = 0, DateTime? at}) =>
          QueueRecord(status: s, enqueuedAt: enqueuedAt, attemptCount: n, lastAttemptAt: at);
      expect(queueShowsAttempt(null), isFalse);
      expect(queueShowsAttempt(r()), isFalse);
      expect(queueShowsAttempt(r(n: 1)), isTrue);
      expect(queueShowsAttempt(r(at: enqueuedAt)), isTrue);
      expect(queueShowsAttempt(r(s: QueueStatus.rejected)), isTrue);
    });

    test('held implies choosable, for every combination', () {
      final statuses = QueueStatus.values;
      final states = [CaptureState.ready, CaptureState.salvaged];
      for (final queuedPresent in [false, true]) {
        for (final status in statuses) {
          for (final n in [0, 1]) {
            for (final at in [null, enqueuedAt]) {
              for (final retention in [null, 'days_30']) {
                for (final skipped in [false, true]) {
                  for (final state in states) {
                    for (final m in [0, 60, 23 * 60 + 56, 24 * 60, 25 * 60]) {
                      final now = enqueuedAt.add(Duration(minutes: m));
                      final queued = queuedPresent
                          ? QueueRecord(
                              status: status,
                              enqueuedAt: enqueuedAt,
                              attemptCount: n,
                              lastAttemptAt: at,
                            )
                          : null;
                      final record = CaptureRecord(
                        captureId: 'a',
                        idempotencyKey: 'k',
                        startedAt: enqueuedAt,
                        state: state,
                        retention: retention,
                        retentionSkippedAt: skipped ? enqueuedAt : null,
                      );
                      // Only a pending record is held by the label/engine.
                      final held =
                          (queued == null || queued.status == QueueStatus.pending) &&
                          !retentionGateOpen(
                            retention: retention,
                            skipped: skipped,
                            enqueuedAt: enqueuedAt,
                            now: now,
                            attempted: queueShowsAttempt(queued),
                          );
                      if (!held) continue;
                      // Held but not in its last minutes: must be choosable.
                      final closes = enqueuedAt.add(retentionGrace).subtract(retentionChoiceCloses);
                      if (!now.isBefore(closes)) continue;
                      expect(
                        retentionChoosable(record, queued, now),
                        isTrue,
                        reason: 'held but not choosable: $queued $retention $skipped $state +${m}m',
                      );
                    }
                  }
                }
              }
            }
          }
        }
      }
    });
  });
}
