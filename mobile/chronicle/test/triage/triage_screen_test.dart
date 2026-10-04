/// CHRN-63's `Done when`, on the real [TriageScreen] at the board's 412 x 915:
///
///  1. a day's memos are triaged in one pass          -> accept-all / one-tap
///  2. a failed item stays visibly pending            -> the failure group
///  3. an accepted ticket is one tap from open        -> the deep-link group
///
/// plus per-item override, the single-memo confirm, hold and the discard undo
/// window. There is no queue and no filesystem here, so nothing needs
/// `runAsync`; the fakes answer in microtasks and [settle] pumps until the
/// controller is idle rather than guessing a duration.
library;

import 'package:chronicle/api/providers.dart';
import 'package:chronicle/features/triage/triage_screen.dart';
import 'package:chronicle/theme/theme.dart';
import 'package:chronicle/theme/tokens.dart';
import 'package:chronicle/triage/triage_controller.dart';
import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'triage_fixture.dart';

class _Harness {
  _Harness(this.api, {this.refs}) : now = DateTime.utc(2026, 10, 3, 20);

  final FakeTriageApi api;
  final FakeReferencesApi? refs;
  DateTime now;
  final opened = <Uri>[];
  late ProviderContainer container;

  Future<void> pump(WidgetTester tester) async {
    tester.view.physicalSize = const Size(412 * 3, 915 * 3);
    tester.view.devicePixelRatio = 3;
    addTearDown(tester.view.reset);
    container = ProviderContainer(overrides: [
      triageApiProvider.overrideWithValue(api),
      referencesApiProvider.overrideWithValue(refs ?? FakeReferencesApi()),
      triageClockProvider.overrideWithValue(() => now),
      urlOpenerProvider.overrideWithValue((url) async {
        opened.add(url);
        return true;
      }),
    ]);
    addTearDown(container.dispose);
    await tester.pumpWidget(UncontrolledProviderScope(
      container: container,
      child: MaterialApp(theme: chronicleTheme(), home: const TriageScreen()),
    ));
    await settle(tester);
  }
}

/// Pumps until nothing is in flight: no row sending and no batch loading.
Future<void> settle(WidgetTester tester) async {
  for (var i = 0; i < 20; i++) {
    await tester.pump(const Duration(milliseconds: 10));
  }
}

Finder k(String key) => find.byKey(ValueKey(key));

void main() {
  group('one pass', () {
    testWidgets('ACCEPT ALL takes every pre-filled memo in a single request and leaves the rest', (tester) async {
      final api = FakeTriageApi([
        item('a'),
        item('d', pre: false, status: 'needs_input', withProposal: false),
        item('b', dest: 'NOTE', verb: 'append'),
        item('c', dest: 'DISCUSSION'),
      ]);
      final h = _Harness(api);
      await h.pump(tester);

      expect(find.textContaining('4 MEMOS · 3 PRE-FILLED · 1 NEED INPUT · 0 FAILED'), findsOneWidget);

      await tester.tap(k('accept-all'));
      await settle(tester);

      expect(api.requests, hasLength(1), reason: 'one POST for the whole pass');
      expect(api.requests.single.map((d) => d.memoId), ['a', 'b', 'c']);
      // The batch key must never set the per-item confirmation an append costs.
      expect(api.requests.single.every((d) => d.confirmEdit == null), isTrue);
      expect(find.textContaining('3 ACCEPTED · 0 EDITED · 0 HELD · 0 DISCARDED · 1 REMAINING'), findsOneWidget);
      // The one that needed input is still waiting, untouched.
      expect(k('edit-d'), findsOneWidget);
    });

    testWidgets('one tap accepts one memo, and an append carries the per-item confirmation', (tester) async {
      final api = FakeTriageApi([item('a', dest: 'NOTE', verb: 'append'), item('b')])
        ..answer = (d) => applied(d.memoId, dest: d.memoId == 'a' ? 'NOTE' : 'TICKET');
      final h = _Harness(api);
      await h.pump(tester);

      await tester.tap(k('accept-a'));
      await settle(tester);

      expect(api.requests, hasLength(1));
      expect(api.requests.single.single.memoId, 'a');
      expect(api.requests.single.single.confirmEdit, isTrue);
      expect(find.text('ACCEPTED · NOTE · CHR-0311'), findsOneWidget);
      expect(k('accept-b'), findsOneWidget, reason: 'the other memo is untouched');
    });

    testWidgets('every control is at least 44 px square', (tester) async {
      final api = FakeTriageApi([item('a'), item('d', pre: false, status: 'needs_input', withProposal: false)]);
      await _Harness(api).pump(tester);

      for (final key in ['accept-all', 'accept-a', 'edit-a', 'hold-a', 'edit-d', 'discard-d', 'open-a']) {
        final size = tester.getSize(k(key));
        expect(size.height, greaterThanOrEqualTo(minTapTarget), reason: '$key height');
        expect(size.width, greaterThanOrEqualTo(minTapTarget), reason: '$key width');
      }
    });

    testWidgets('a full batch reads "N+" and loads the next memos when these are decided', (tester) async {
      final api = FakeTriageApi([item('a'), item('b')])..limit = 2;
      await _Harness(api).pump(tester);
      expect(find.textContaining('2+ MEMOS'), findsOneWidget);

      api.batch = [item('c')];
      await tester.tap(k('accept-all'));
      await settle(tester);

      expect(api.batchReads, 2, reason: 'the next screen of memos was read');
      expect(k('accept-c'), findsOneWidget);
    });
  });

  group('a failed item stays visibly pending', () {
    testWidgets('failed, refused and stale each stay on screen with the server\'s reason', (tester) async {
      final api = FakeTriageApi([item('ok'), item('bad'), item('no'), item('old')])
        ..answer = (d) => switch (d.memoId) {
              'bad' => gen.TriageResult(memoId: 'bad', status: 'failed', reason: 'Switchyard did not answer.'),
              'no' => gen.TriageResult(memoId: 'no', status: 'refused', reason: 'Project CHRN is archived.'),
              'old' => gen.TriageResult(memoId: 'old', status: 'stale'),
              _ => applied(d.memoId),
            };
      await _Harness(api).pump(tester);

      await tester.tap(k('accept-all'));
      await settle(tester);

      // The applied one is out of the pending set; the other three are not.
      expect(find.text('ACCEPTED · TICKET'), findsOneWidget);
      expect(find.text('FAILED · STILL PENDING'), findsOneWidget);
      expect(find.text('Switchyard did not answer.'), findsOneWidget);
      expect(find.text('REFUSED · STILL PENDING'), findsOneWidget);
      expect(find.text('Project CHRN is archived.'), findsOneWidget);
      // `stale` re-reads the batch, and the row is back to being decidable.
      expect(api.batchReads, 2);
      expect(find.textContaining('2 FAILED'), findsOneWidget, reason: 'header counts the failures');
      // And the failed one can be tried again as it was.
      expect(k('accept-bad'), findsOneWidget);
    });

    testWidgets('a retry that lands takes the row out of the pending set', (tester) async {
      var fail = true;
      final api = FakeTriageApi([item('a')])
        ..answer = (d) => fail ? gen.TriageResult(memoId: d.memoId, status: 'failed') : applied(d.memoId);
      await _Harness(api).pump(tester);

      await tester.tap(k('accept-a'));
      await settle(tester);
      expect(find.text('FAILED · STILL PENDING'), findsOneWidget);

      fail = false;
      await tester.tap(k('accept-a'));
      await settle(tester);

      expect(find.text('FAILED · STILL PENDING'), findsNothing);
      expect(find.text('ACCEPTED · TICKET'), findsOneWidget);
      expect(api.requests, hasLength(2));
    });

    testWidgets('an unreachable server leaves every row pending and says to retry', (tester) async {
      final api = FakeTriageApi([item('a'), item('b')]);
      await _Harness(api).pump(tester);

      api.unreachable = true;
      await tester.tap(k('accept-all'));
      await settle(tester);

      expect(find.text('FAILED · STILL PENDING'), findsNWidgets(2));
      expect(find.textContaining('Chronicle could not be reached'), findsNWidgets(2));
      expect(k('accept-a'), findsOneWidget);
      expect(k('accept-b'), findsOneWidget);
    });

    testWidgets('a batch that cannot be read says so instead of showing an empty day', (tester) async {
      final api = FakeTriageApi([])..unreachable = true;
      await _Harness(api).pump(tester);

      expect(k('load-error'), findsOneWidget);
      expect(k('empty'), findsNothing);
    });
  });

  group('override', () {
    testWidgets('an edit sends the whole override, not a patch, and validates blanks first', (tester) async {
      final api = FakeTriageApi([item('a', dest: 'DISCUSSION')]);
      await _Harness(api).pump(tester);

      await tester.tap(k('edit-a'));
      await tester.pumpAndSettle();
      await tester.tap(k('dest-TICKET'));
      await tester.pump();
      // The ticket needs a project key; the proposal was a discussion without one.
      await tester.tap(k('editor-confirm'));
      await tester.pump();
      expect(k('editor-error'), findsOneWidget);
      expect(api.requests, isEmpty, reason: 'a blank the server would refuse is not sent');

      await tester.enterText(k('editor-project'), 'chrn');
      await tester.enterText(k('editor-title'), 'Make it a ticket');
      await tester.tap(k('editor-confirm'));
      await tester.pumpAndSettle();
      await settle(tester);

      final d = api.requests.single.single;
      expect(d.memoId, 'a');
      expect(d.proposalOverride!.destination, 'TICKET');
      expect(d.proposalOverride!.title, 'Make it a ticket');
      expect(d.proposalOverride!.projectKey, 'CHRN');
      expect(d.proposalOverride!.ticketType, 'task');
      expect(d.proposalOverride!.description, isNotEmpty);
      expect(find.text('ACCEPTED · TICKET · EDITED'), findsOneWidget);
    });

    testWidgets('a memo with no proposal can only be decided through the editor', (tester) async {
      final api = FakeTriageApi([item('d', pre: false, status: 'needs_input', withProposal: false)]);
      await _Harness(api).pump(tester);

      expect(k('accept-d'), findsNothing);
      await tester.tap(k('edit-d'));
      await tester.pumpAndSettle();
      await tester.enterText(k('editor-title'), 'A note');
      await tester.enterText(k('editor-page'), 'estate / notes');
      await tester.tap(k('editor-confirm'));
      await tester.pumpAndSettle();
      await settle(tester);

      final o = api.requests.single.single.proposalOverride!;
      expect(o.destination, 'NOTE');
      expect(o.pagePath, 'estate/notes', reason: 'the board\'s spaced path is normalised');
      expect(o.verb, gen.OverrideVerbEnum.create);
    });

    testWidgets('the one-off confirm shows the transcript and accepts with the confirmation', (tester) async {
      final api = FakeTriageApi([item('a', dest: 'NOTE', verb: 'supersede')]);
      await _Harness(api).pump(tester);

      await tester.tap(k('open-a'));
      await tester.pumpAndSettle();
      expect(k('confirm-excerpt-a'), findsOneWidget);
      expect(find.textContaining('WRITES INTO A NOTE SOMEBODY ALREADY WROTE'), findsOneWidget);

      await tester.tap(k('confirm-accept-a'));
      await tester.pumpAndSettle();
      await settle(tester);

      expect(api.requests.single.single.confirmEdit, isTrue);
    });
  });

  group('hold and discard', () {
    testWidgets('hold parks a row and release brings it back', (tester) async {
      final api = FakeTriageApi([item('a')]);
      await _Harness(api).pump(tester);

      await tester.tap(k('hold-a'));
      await settle(tester);
      expect(api.held, ['a']);
      expect(find.textContaining('HELD · NOT NOW'), findsOneWidget);

      await tester.tap(k('release-a'));
      await settle(tester);
      expect(api.released, ['a']);
      expect(k('accept-a'), findsOneWidget);
    });

    testWidgets('a discard is held back for the undo window, and leaving the screen sends it', (tester) async {
      final api = FakeTriageApi([item('a', dest: 'DISCARD')]);
      final h = _Harness(api);
      await h.pump(tester);

      await tester.tap(k('discard-a'));
      await settle(tester);
      expect(find.text('DISCARDED · UNDO 10 MIN'), findsOneWidget);
      expect(api.requests, isEmpty, reason: 'nothing is sent while the window is open');

      // Undo inside the window: it is simply still waiting.
      await tester.tap(k('undo-a'));
      await settle(tester);
      expect(k('discard-a'), findsOneWidget);
      expect(api.requests, isEmpty);

      // Discard again, then leave the screen: the window ends and it is sent.
      await tester.tap(k('discard-a'));
      await settle(tester);
      await tester.pumpWidget(const SizedBox());
      await settle(tester);
      expect(api.requests, hasLength(1));
      expect(api.requests.single.single.proposalOverride!.destination, 'DISCARD');
    });

    testWidgets('a discard whose window has closed is sent by the ticker', (tester) async {
      final api = FakeTriageApi([item('a', dest: 'DISCARD')]);
      final h = _Harness(api);
      await h.pump(tester);

      await tester.tap(k('discard-a'));
      await settle(tester);
      h.now = h.now.add(const Duration(minutes: 11));
      await tester.pump(const Duration(seconds: 16));
      await settle(tester);

      expect(api.requests, hasLength(1));
      expect(find.text('DISCARDED'), findsOneWidget);
    });
  });

  group('an accepted ticket is one tap from open', () {
    testWidgets('the card is coral-linked, says LINKED · NOT COPIED, shows its age, and one tap opens it', (tester) async {
      final api = FakeTriageApi([item('a')]);
      final h = _Harness(api);
      await h.pump(tester);

      await tester.tap(k('accept-a'));
      await settle(tester);

      expect(find.text('CHRN-900 · IN PROGRESS'), findsOneWidget, reason: 'the upstream\'s own state word');
      expect(find.textContaining('LINKED · NOT COPIED · AS OF 4 MIN AGO'), findsOneWidget);
      final border = (tester.widget<Container>(find.descendant(
        of: k('open-ticket-CHRN-900'),
        matching: find.byType(Container),
      ).first).decoration! as BoxDecoration).border! as Border;
      expect(border.top.color, refSwitchyard);
      expect(tester.getSize(k('open-ticket-CHRN-900')).height, greaterThanOrEqualTo(minTapTarget));

      expect(h.opened, isEmpty, reason: 'accepting does not navigate away by itself');
      await tester.tap(k('open-ticket-CHRN-900'));
      await settle(tester);
      expect(h.opened, [Uri.parse('https://switchyard.example.com/t/CHRN-900')], reason: 'one tap');
    });

    testWidgets('when Switchyard is unreachable the card says so, and the link still opens', (tester) async {
      final api = FakeTriageApi([item('a')]);
      final refs = FakeReferencesApi()..unreachable = true;
      final h = _Harness(api, refs: refs);
      await h.pump(tester);

      await tester.tap(k('accept-a'));
      await settle(tester);

      expect(find.text('CHRN-900 · SWITCHYARD UNREACHABLE'), findsOneWidget);
      expect(find.textContaining('AS OF'), findsNothing, reason: 'no confident stale age on an unreachable card');
      await tester.tap(k('open-ticket-CHRN-900'));
      await settle(tester);
      expect(h.opened, hasLength(1));
    });

    testWidgets('a NOTE route gets no ticket card', (tester) async {
      final api = FakeTriageApi([item('a', dest: 'NOTE', verb: 'create')])
        ..answer = (d) => applied(d.memoId, dest: 'NOTE');
      await _Harness(api).pump(tester);

      await tester.tap(k('accept-a'));
      await settle(tester);

      expect(find.text('ACCEPTED · NOTE · CHR-0311'), findsOneWidget);
      expect(find.byType(InkWell).evaluate().where((e) => e.widget.key.toString().contains('open-ticket')), isEmpty);
    });
  });
}
