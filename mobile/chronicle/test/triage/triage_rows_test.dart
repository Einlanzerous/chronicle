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

  group('FILE', () {
    test('its label is the count of what it would send', () {
      expect(fileLabel(3, 1), 'FILE 3 · DISCARD 1');
      expect(fileLabel(3, 0), 'FILE 3');
      expect(fileLabel(0, 2), 'DISCARD 2');
      expect(fileLabel(0, 0), 'NOTHING TO FILE');
    });

    test('takes confident rows as shown, a proposed discard as a discard, and nothing it cannot stand behind', () {
      expect(filingOf(TriageRow(item: item('a'))), Filing.file);
      expect(filingOf(TriageRow(item: item('a', dest: 'DISCARD'))), Filing.discard);
      expect(filingOf(TriageRow(item: item('a', pre: false))), Filing.none);
      expect(filingOf(TriageRow(item: item('a', pre: false), confirmed: true)), Filing.file);
      expect(filingOf(TriageRow(item: item('a', pre: false, withProposal: false, status: 'needs_input'))), Filing.none);
      expect(filingOf(TriageRow(item: item('a'), local: const Sending())), Filing.none);
    });

    test('a failed row is filed again, a refused one waits for a changed decision', () {
      expect(filingOf(TriageRow(item: item('a'), local: const Problem(ProblemStatus.failed, 'x'))), Filing.file);
      expect(filingOf(TriageRow(item: item('a'), local: const Problem(ProblemStatus.refused, 'x'))), Filing.none);
      final changed = TriageRow(
        item: item('a'),
        local: const Problem(ProblemStatus.refused, 'x'),
        draft: draftForLane(TriageRow(item: item('a')), 'DISCUSSION'),
      );
      expect(filingOf(changed), Filing.file);
    });
  });

  group('a lane tap', () {
    test('carries the title and text, and leaves blank what the new destination needs', () {
      final row = TriageRow(item: item('a')); // a TICKET proposal for CHRN
      final note = draftForLane(row, 'NOTE')!;
      expect(note.title, 'Title a');
      expect(note.text, 'Do the thing.');
      expect(validateDraft(note), 'A note must name the page it belongs on.');
      final disc = draftForLane(row, 'DISCUSSION')!;
      expect(validateDraft(disc), isNull);
      expect(draftForLane(row, 'TICKET'), isNull, reason: 'the proposal\'s own lane is "the Scribe\'s pick"');
    });

    test('a discussion proposal cannot supply a ticket\'s project', () {
      final t = draftForLane(TriageRow(item: item('a', dest: 'DISCUSSION')), 'TICKET')!;
      expect(validateDraft(t), contains('project key'));
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
