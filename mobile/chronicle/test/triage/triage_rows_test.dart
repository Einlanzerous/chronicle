import 'package:chronicle/triage/triage_rows.dart';
import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter_test/flutter_test.dart';

import 'triage_fixture.dart';

void main() {
  final now = DateTime(2026, 10, 3, 20);

  group('recorded time is read defensively', () {
    // CHRN-118 shipped `recorded_at` bounded only by UTC year 100..9900, and
    // nothing upstream filters an implausible-but-in-range one.
    test('a year-100 or year-9900 clock is shown as unknown, never as a date', () {
      expect(formatClock(DateTime.utc(100, 1, 1, 12), now), '--:--');
      expect(formatClock(DateTime.utc(9900, 1, 1, 12), now), '--:--');
      expect(plausibleTime(DateTime.utc(1999, 12, 31), now), isNull);
    });

    test('a believable time reads as local clock time', () {
      final at = DateTime(2026, 10, 3, 14, 5);
      expect(formatClock(at, now), '14:05');
    });

    test('the batch date line skips unbelievable times and says nothing when none are believable', () {
      expect(batchDayLabel([DateTime.utc(100), DateTime.utc(9900)], now), isNull);
      expect(batchDayLabel([DateTime.utc(100), DateTime(2026, 10, 3, 9)], now), 'TODAY');
      expect(batchDayLabel([DateTime(2026, 10, 2, 9), DateTime(2026, 10, 3, 9)], now), 'YESTERDAY');
      expect(batchDayLabel([DateTime(2026, 9, 29, 9)], now), 'TUE 29 SEP');
    });
  });

  group('rows', () {
    test('ACCEPT ALL takes only untouched, server-confident, pre-filled rows', () {
      expect(isPrefilled(TriageRow(item: item('a'))), isTrue);
      expect(isPrefilled(TriageRow(item: item('a', pre: false))), isFalse);
      expect(isPrefilled(TriageRow(item: item('a', dest: 'DISCARD'))), isFalse);
      expect(
        isPrefilled(TriageRow(item: item('a'), local: const Problem(ProblemStatus.failed, 'x'))),
        isFalse,
        reason: 'a row the server already answered about is never swept up again',
      );
    });

    test('only confirm_edit on a single append or supersede', () {
      final append = item('a', dest: 'NOTE', verb: 'append');
      expect(acceptDecision(append, single: true).confirmEdit, isTrue);
      expect(acceptDecision(append, single: false).confirmEdit, isNull);
      expect(acceptDecision(item('b', dest: 'NOTE', verb: 'create'), single: true).confirmEdit, isNull);
    });

    test('only applied takes a row out of the batch', () {
      for (final status in ['needs_input', 'stale', 'refused', 'failed']) {
        final out = applyResult(
          TriageRow(item: item('a')),
          gen.TriageResult(memoId: 'a', status: status),
          edited: false,
          now: now,
        );
        expect(out.local, isA<Problem>(), reason: status);
        expect(isWaiting(out), isTrue, reason: status);
      }
      final landed = applyResult(TriageRow(item: item('a')), applied('a'), edited: false, now: now);
      expect(landed.local, isA<Accepted>());
      expect(isWaiting(landed), isFalse);
    });

    test('mergeBatch keeps decided rows, refreshes waiting ones and drops ones decided elsewhere', () {
      final rows = [
        TriageRow(item: item('done'), local: Accepted(edited: false, result: applied('done'), at: now)),
        TriageRow(item: item('wait')),
        TriageRow(item: item('gone')),
      ];
      final merged = mergeBatch(rows, [item('wait'), item('new')]);
      expect(merged.map((r) => r.memoId), ['done', 'wait', 'new']);
      expect(merged.first.local, isA<Accepted>());
    });
  });
}
