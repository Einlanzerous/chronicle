/// CHRN-63's `Done when`, on the real [TriageScreen] at the board's 412 x 915
/// (board 1b, B2 -- "the lane"; board 1a frame 06 for the card):
///
///  1. a day's memos are triaged in one pass          -> file-in-one-tap
///  2. a failed item stays visibly pending            -> the failure group
///  3. an accepted ticket is one tap from open        -> the deep-link group
///
/// plus the lane override, the discard row, hold and the single-memo confirm.
/// There is no queue and no filesystem here, so nothing needs `runAsync`; the
/// fakes answer in microtasks and [settle] pumps until the controller is idle
/// rather than guessing a duration.
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

String fileText(WidgetTester tester) =>
    (tester.widget<Text>(find.descendant(of: k('file'), matching: find.byType(Text)))).data!;

/// The colour a lane is filled with.
Color laneFill(WidgetTester tester, String id, String lane) {
  final box = find.descendant(of: k('lane-$id-$lane'), matching: find.byType(Container)).first;
  return tester.widget<Container>(box).color!;
}

void main() {
  group('one pass', () {
    testWidgets('the Scribe\'s picks are filled in, and FILE is the one tap that commits them', (tester) async {
      final api = FakeTriageApi([
        item('a'),
        item('b', dest: 'NOTE', verb: 'create'),
        item('c', dest: 'DISCUSSION'),
        item('x', dest: 'DISCARD'),
      ])
        ..answer = (d) => applied(d.memoId, dest: const {'a': 'TICKET', 'b': 'NOTE', 'c': 'DISCUSSION'}[d.memoId]!);
      final h = _Harness(api);
      await h.pump(tester);

      expect(find.text('4 MEMOS · SCRIBE PRE-FILLED'), findsOneWidget);
      expect(fileText(tester), 'FILE 3 · DISCARD 1');
      // Each pick is filled in its destination's colour; the other lanes are dim.
      expect(laneFill(tester, 'a', 'TICKET'), refSwitchyard);
      expect(laneFill(tester, 'a', 'NOTE'), chLaneOff);
      expect(laneFill(tester, 'b', 'NOTE'), chSignal);
      expect(laneFill(tester, 'c', 'DISCUSSION'), chDiscussion);
      // The discard is a bordered strip with the reason, in place of the lanes.
      expect(k('discard-strip-x'), findsOneWidget);
      expect(k('lane-x-TICKET'), findsNothing);
      expect(api.requests, isEmpty, reason: 'nothing is sent until FILE');

      await tester.tap(k('file'));
      await settle(tester);

      expect(api.requests, hasLength(1), reason: 'one POST for the whole pass');
      expect(api.requests.single.map((d) => d.memoId), ['a', 'b', 'c']);
      expect(api.requests.single.every((d) => d.proposalOverride == null), isTrue, reason: 'filed as shown');
      expect(api.requests.single.every((d) => d.confirmEdit == null), isTrue);
      expect(find.text('TICKET CREATED'), findsOneWidget);
      expect(find.text('NOTE CREATED · CHR-0311'), findsOneWidget);
      expect(find.text('DISCUSSION OPENED'), findsOneWidget);
      // The discard enters its undo window rather than being sent.
      expect(find.text('DISCARDED · UNDO 10 MIN'), findsOneWidget);
      expect(fileText(tester), 'NOTHING TO FILE');
    });

    testWidgets('a proposed discard is dimmed and discarded by FILE after its undo window', (tester) async {
      final api = FakeTriageApi([item('x', dest: 'DISCARD')]);
      await _Harness(api).pump(tester);
      expect(fileText(tester), 'DISCARD 1');

      await tester.tap(k('file'));
      await settle(tester);
      expect(api.requests, isEmpty, reason: 'nothing is sent while the window is open');

      await tester.tap(k('undo-x'));
      await settle(tester);
      expect(k('discard-strip-x'), findsOneWidget, reason: 'undone: it is simply still waiting');

      await tester.tap(k('file'));
      await settle(tester);
      // Leaving the screen ends the window and sends it.
      await tester.pumpWidget(const SizedBox());
      await settle(tester);
      expect(api.requests, hasLength(1));
      expect(api.requests.single.single.proposalOverride!.destination, 'DISCARD');
    });

    testWidgets('a memo with no proposal, and a low-confidence one, are not filed blind', (tester) async {
      final api = FakeTriageApi([
        item('a'),
        item('n', pre: false, status: 'needs_input', withProposal: false),
        item('l', pre: false),
      ]);
      await _Harness(api).pump(tester);

      expect(fileText(tester), 'FILE 1');
      expect(find.textContaining('NO PROPOSAL'), findsOneWidget);
      expect(find.textContaining('LOW CONFIDENCE · TAP THE LANE TO CONFIRM'), findsOneWidget);

      // Tapping the already-picked lane is the person's confirmation.
      await tester.tap(k('lane-l-TICKET'));
      await tester.pump();
      expect(fileText(tester), 'FILE 2');
      expect(find.textContaining('LOW CONFIDENCE'), findsNothing);
    });

    testWidgets('ACCEPT ALL commits only the untouched pre-filled set; FILE commits the lanes as they stand', (tester) async {
      final api = FakeTriageApi([
        item('a'),
        item('b', dest: 'NOTE', verb: 'append'),
        item('c', dest: 'DISCUSSION'),
        item('x', dest: 'DISCARD'),
      ]);
      await _Harness(api).pump(tester);

      await tester.tap(k('lane-c-TICKET')); // c is overridden, so it is not "pre-filled" any more
      await tester.pump();
      await tester.tap(k('accept-all'));
      await settle(tester);

      expect(api.requests, hasLength(1));
      expect(api.requests.single.map((d) => d.memoId), ['a', 'b']);
      expect(api.requests.single.every((d) => d.confirmEdit == null), isTrue, reason: 'a batch key never confirms an append');
      expect(k('lane-c-TICKET'), findsOneWidget, reason: 'the overridden row is still waiting');
      expect(k('discard-strip-x'), findsOneWidget, reason: 'and so is the discard');
    });

    testWidgets('every tap target is at least 44 px', (tester) async {
      final api = FakeTriageApi([item('a'), item('n', pre: false, status: 'needs_input', withProposal: false)]);
      await _Harness(api).pump(tester);

      for (final key in [
        'accept-all',
        'file',
        'open-a',
        'lane-a-TICKET',
        'lane-a-NOTE',
        'lane-a-DISCUSSION',
        'lane-n-TICKET',
      ]) {
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
      await tester.tap(k('file'));
      await settle(tester);

      expect(api.batchReads, 2, reason: 'the next screen of memos was read');
      expect(k('lane-c-TICKET'), findsOneWidget);
    });
  });

  group('override is a tap on a different lane', () {
    testWidgets('a lane the proposal can supply is staged, shown as the person\'s, and sent by FILE as an override', (tester) async {
      final api = FakeTriageApi([item('a')])..answer = (d) => applied(d.memoId, dest: 'DISCUSSION');
      await _Harness(api).pump(tester);

      await tester.tap(k('lane-a-DISCUSSION'));
      await tester.pump();
      expect(laneFill(tester, 'a', 'DISCUSSION'), chDiscussion);
      expect(laneFill(tester, 'a', 'TICKET'), chLaneOff);
      expect(find.textContaining('YOUR CHOICE'), findsOneWidget);
      expect(fileText(tester), 'FILE 1');
      expect(api.requests, isEmpty, reason: 'a lane tap sends nothing by itself');

      await tester.tap(k('file'));
      await settle(tester);

      final d = api.requests.single.single;
      expect(d.proposalOverride!.destination, 'DISCUSSION');
      expect(d.proposalOverride!.title, 'Title a');
      expect(d.proposalOverride!.openingPost, 'Do the thing.', reason: 'the text carries across');
      expect(find.text('DISCUSSION OPENED · EDITED'), findsOneWidget);
    });

    testWidgets('a lane the proposal cannot fill says it needs input and is not filed until it has it', (tester) async {
      final api = FakeTriageApi([item('a'), item('b', dest: 'DISCUSSION')]);
      await _Harness(api).pump(tester);

      // TICKET -> NOTE: the ticket proposal named no page, and a note must.
      await tester.tap(k('lane-a-NOTE'));
      await tester.pump();
      expect(find.textContaining('NEEDS INPUT · A note must name the page it belongs on.'), findsOneWidget);
      expect(fileText(tester), 'FILE 1', reason: 'only b remains');
      // DISCUSSION -> TICKET: no project key to carry.
      await tester.tap(k('lane-b-TICKET'));
      await tester.pump();
      expect(find.textContaining('A ticket needs a project key'), findsOneWidget);
      expect(fileText(tester), 'NOTHING TO FILE');

      // Supply the page through the row's own tap, and it joins FILE.
      await tester.tap(k('open-a'));
      await tester.pumpAndSettle();
      await tester.tap(k('confirm-edit-a'));
      await tester.pumpAndSettle();
      expect(find.byKey(const ValueKey('editor-page')), findsOneWidget);
      await tester.enterText(k('editor-page'), 'estate / notes');
      await tester.tap(k('editor-confirm'));
      await tester.pumpAndSettle();
      expect(fileText(tester), 'FILE 1');

      await tester.tap(k('file'));
      await settle(tester);
      final o = api.requests.single.single.proposalOverride!;
      expect(o.destination, 'NOTE');
      expect(o.pagePath, 'estate/notes');
      expect(o.verb, gen.OverrideVerbEnum.create);
      expect(o.title, 'Title a');
    });

    testWidgets('tapping the proposal\'s own lane again puts the row back to the Scribe\'s pick', (tester) async {
      final api = FakeTriageApi([item('a')]);
      await _Harness(api).pump(tester);

      await tester.tap(k('lane-a-DISCUSSION'));
      await tester.pump();
      await tester.tap(k('lane-a-TICKET'));
      await tester.pump();

      expect(find.textContaining('YOUR CHOICE'), findsNothing);
      await tester.tap(k('file'));
      await settle(tester);
      expect(api.requests.single.single.proposalOverride, isNull);
    });

    testWidgets('a memo with no proposal is decided by a lane tap plus what it still needs', (tester) async {
      final api = FakeTriageApi([item('n', pre: false, status: 'needs_input', withProposal: false)]);
      await _Harness(api).pump(tester);

      await tester.tap(k('lane-n-DISCUSSION'));
      await tester.pump();
      expect(find.textContaining('NEEDS INPUT · A title is required.'), findsOneWidget);
      await tester.tap(k('open-n'));
      await tester.pumpAndSettle();
      await tester.tap(k('confirm-edit-n'));
      await tester.pumpAndSettle();
      await tester.enterText(k('editor-title'), 'Open question');
      await tester.tap(k('editor-confirm'));
      await tester.pumpAndSettle();

      await tester.tap(k('file'));
      await settle(tester);
      final o = api.requests.single.single.proposalOverride!;
      expect(o.destination, 'DISCUSSION');
      expect(o.openingPost, isNotEmpty, reason: 'the transcript is the starting text');
    });

    testWidgets('the one-off confirm shows the transcript and accepts alone, with the append confirmation', (tester) async {
      final api = FakeTriageApi([item('a', dest: 'NOTE', verb: 'supersede'), item('b')]);
      await _Harness(api).pump(tester);

      await tester.tap(k('open-a'));
      await tester.pumpAndSettle();
      expect(k('confirm-excerpt-a'), findsOneWidget);
      expect(find.textContaining('WRITES INTO A NOTE SOMEBODY ALREADY WROTE'), findsOneWidget);
      await tester.tap(k('confirm-accept-a'));
      await tester.pumpAndSettle();
      await settle(tester);

      expect(api.requests, hasLength(1));
      expect(api.requests.single.map((d) => d.memoId), ['a'], reason: 'this memo, on its own');
      expect(api.requests.single.single.confirmEdit, isTrue);
      expect(fileText(tester), 'FILE 1', reason: 'b is still waiting for FILE');
    });

    testWidgets('the confirm sheet\'s DISCARD stages a discard; HOLD parks the row until released', (tester) async {
      final api = FakeTriageApi([item('a'), item('b')]);
      await _Harness(api).pump(tester);

      await tester.tap(k('open-a'));
      await tester.pumpAndSettle();
      await tester.tap(k('confirm-discard-a'));
      await tester.pumpAndSettle();
      expect(fileText(tester), 'FILE 1 · DISCARD 1');
      expect(api.requests, isEmpty);

      await tester.tap(k('open-b'));
      await tester.pumpAndSettle();
      await tester.tap(k('confirm-hold-b'));
      await tester.pumpAndSettle();
      await settle(tester);
      expect(api.held, ['b']);
      expect(find.textContaining('HELD · NOT NOW'), findsOneWidget);

      await tester.tap(k('release-b'));
      await settle(tester);
      expect(api.released, ['b']);
      expect(k('lane-b-TICKET'), findsOneWidget);
    });

    testWidgets('a discard whose window has closed is sent by the ticker', (tester) async {
      final api = FakeTriageApi([item('x', dest: 'DISCARD')]);
      final h = _Harness(api);
      await h.pump(tester);

      await tester.tap(k('file'));
      await settle(tester);
      h.now = h.now.add(const Duration(minutes: 11));
      await tester.pump(const Duration(seconds: 16));
      await settle(tester);

      expect(api.requests, hasLength(1));
      expect(find.text('DISCARDED'), findsOneWidget);
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

      await tester.tap(k('file'));
      await settle(tester);

      expect(find.text('TICKET CREATED'), findsOneWidget);
      expect(find.text('FAILED · STILL PENDING'), findsOneWidget);
      expect(find.text('Switchyard did not answer.'), findsOneWidget);
      expect(find.text('REFUSED · STILL PENDING'), findsOneWidget);
      expect(find.text('Project CHRN is archived.'), findsOneWidget);
      // `stale` re-reads the batch, and the row is back to being decidable.
      expect(api.batchReads, 2);
      expect(find.textContaining('2 FAILED'), findsOneWidget, reason: 'the header counts the failures');
    });

    testWidgets('FILE is the retry: a failed row is counted in it and a retry that lands clears it', (tester) async {
      var fail = true;
      final api = FakeTriageApi([item('a')])
        ..answer = (d) => fail ? gen.TriageResult(memoId: d.memoId, status: 'failed') : applied(d.memoId);
      await _Harness(api).pump(tester);

      await tester.tap(k('file'));
      await settle(tester);
      expect(find.text('FAILED · STILL PENDING'), findsOneWidget);
      expect(fileText(tester), 'FILE 1', reason: 'still pending, so still counted');

      fail = false;
      await tester.tap(k('file'));
      await settle(tester);

      expect(find.text('FAILED · STILL PENDING'), findsNothing);
      expect(find.text('TICKET CREATED'), findsOneWidget);
      expect(api.requests, hasLength(2));
    });

    testWidgets('a refused row is not filed again until the person changes it', (tester) async {
      final api = FakeTriageApi([item('a')])
        ..answer = (d) => gen.TriageResult(memoId: d.memoId, status: 'refused', reason: 'Project CHRN is archived.');
      await _Harness(api).pump(tester);

      await tester.tap(k('file'));
      await settle(tester);

      expect(find.text('REFUSED · STILL PENDING'), findsOneWidget);
      expect(fileText(tester), 'NOTHING TO FILE');
      // One lane tap changes the decision, and it is filed again.
      await tester.tap(k('lane-a-DISCUSSION'));
      await tester.pump();
      expect(fileText(tester), 'FILE 1');
    });

    testWidgets('an unreachable server leaves every row pending and says to retry', (tester) async {
      final api = FakeTriageApi([item('a'), item('b')]);
      await _Harness(api).pump(tester);

      api.unreachable = true;
      await tester.tap(k('file'));
      await settle(tester);

      expect(find.text('FAILED · STILL PENDING'), findsNWidgets(2));
      expect(find.textContaining('Chronicle could not be reached'), findsNWidgets(2));
      expect(fileText(tester), 'FILE 2');
    });

    testWidgets('a batch that cannot be read says so instead of showing an empty day', (tester) async {
      final api = FakeTriageApi([])..unreachable = true;
      await _Harness(api).pump(tester);

      expect(k('load-error'), findsOneWidget);
      expect(k('empty'), findsNothing);
    });
  });

  group('an accepted ticket is one tap from open', () {
    testWidgets('board 1a 06\'s card: LINKED · NOT COPIED, coral rule, upstream state and age, one tap opens it', (tester) async {
      final api = FakeTriageApi([item('a')]);
      final h = _Harness(api);
      await h.pump(tester);

      await tester.tap(k('file'));
      await settle(tester);

      expect(find.text('CHRN-900 · IN PROGRESS'), findsOneWidget, reason: 'the upstream\'s own state word');
      expect(find.text('A ticket the memo made'), findsOneWidget);
      expect(find.text('LINKED · NOT COPIED · AS OF 4 MIN AGO'), findsOneWidget);
      expect(find.text('OPEN ↗'), findsOneWidget);
      final card = find.descendant(of: k('open-ticket-CHRN-900'), matching: find.byType(Container)).first;
      final border = (tester.widget<Container>(card).decoration! as BoxDecoration).border! as Border;
      expect(border.left.color, refSwitchyard);
      expect(tester.getSize(k('open-ticket-CHRN-900')).height, greaterThanOrEqualTo(minTapTarget));

      expect(h.opened, isEmpty, reason: 'filing does not navigate away by itself');
      await tester.tap(k('open-ticket-CHRN-900'));
      await settle(tester);
      expect(h.opened, [Uri.parse('https://switchyard.example.com/t/CHRN-900')], reason: 'one tap');
    });

    testWidgets('when Switchyard is unreachable the card says so, and the link still opens', (tester) async {
      final api = FakeTriageApi([item('a')]);
      final refs = FakeReferencesApi()..unreachable = true;
      final h = _Harness(api, refs: refs);
      await h.pump(tester);

      await tester.tap(k('file'));
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

      await tester.tap(k('file'));
      await settle(tester);

      expect(find.text('NOTE CREATED · CHR-0311'), findsOneWidget);
      expect(find.textContaining('LINKED · NOT COPIED'), findsNothing);
    });
  });
}
