/// CHRN-64's `Done when`, clause by clause, against the rule in
/// `lib/notify/nudge.dart`: a batch produces exactly one notification, and no
/// notification arrives for a memo already triaged on the web. (The third
/// clause, the tap, is platform plumbing and is proved on a device.)
library;

import 'package:chronicle/notify/nudge.dart';
import 'package:chronicle/notify/nudge_surface.dart';
import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../triage/triage_fixture.dart';

class FakeSurface implements NudgeSurface {
  final posts = <String>[];
  final updates = <String>[];
  int cancels = 0;
  bool enabled = true;
  bool visible = false;

  @override
  Future<bool> post({required int count, required bool atLeast}) async {
    if (!enabled) return false;
    posts.add(nudgeTitle(count: count, atLeast: atLeast));
    visible = true;
    return true;
  }

  @override
  Future<void> update({required int count, required bool atLeast}) async =>
      updates.add(nudgeTitle(count: count, atLeast: atLeast));

  @override
  Future<void> cancel() async {
    cancels++;
    visible = false;
  }

  @override
  Future<bool> showing() async => visible;
}

gen.TriageBatch batchOf(List<gen.BatchItem> items, {int limit = 25}) =>
    gen.TriageBatch(items: items, limit: limit);

List<gen.BatchItem> memos(int n, {String prefix = 'm'}) =>
    [for (var i = 0; i < n; i++) item('$prefix$i')];

gen.LinkState decided() => gen.LinkState(
      destination: 'TICKET',
      state: 'in_flight',
      decidedAt: DateTime.utc(2026, 10, 5, 17),
    );

final evening = DateTime(2026, 10, 5, 18, 5);

void main() {
  late SharedPreferences prefs;
  late FakeSurface surface;
  late gen.TriageBatch server;

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    prefs = await SharedPreferences.getInstance();
    surface = FakeSurface();
    server = batchOf([]);
  });

  Future<NudgeDecision> pass(DateTime now) => runNudgePass(
        fetch: () async => server,
        surface: surface,
        prefs: prefs,
        now: now,
      );

  group('a batch produces exactly one notification', () {
    test('nine waiting is one notification that says nine, however often the wake fires', () async {
      server = batchOf(memos(9));
      for (var minute = 5; minute < 240; minute += 15) {
        await pass(DateTime(2026, 10, 5, 18).add(Duration(minutes: minute)));
      }
      expect(surface.posts, ['9 memos waiting for triage']);
    });

    test('not through the afternoon: the same batch waits for the evening', () async {
      server = batchOf(memos(9));
      await pass(DateTime(2026, 10, 5, 13, 30));
      await pass(DateTime(2026, 10, 5, 17, 59));
      expect(surface.posts, isEmpty);
      await pass(evening);
      expect(surface.posts, hasLength(1));
    });

    test('a wake Doze deferred past the window does not buzz at night', () async {
      server = batchOf(memos(3));
      await pass(DateTime(2026, 10, 5, 22, 0));
      await pass(DateTime(2026, 10, 6, 1, 30));
      expect(surface.posts, isEmpty);
      await pass(DateTime(2026, 10, 6, 18, 20));
      expect(surface.posts, ['3 memos waiting for triage']);
    });

    test('the same batch left untouched is not announced again the next evening', () async {
      server = batchOf(memos(4));
      await pass(evening);
      surface.visible = false; // dismissed
      await pass(evening.add(const Duration(days: 1)));
      await pass(evening.add(const Duration(days: 2)));
      expect(surface.posts, hasLength(1));
    });

    test('a memo that arrives later the same evening does not buzz a second time', () async {
      server = batchOf(memos(4));
      await pass(evening);
      surface.visible = false;
      server = batchOf([...memos(4), item('late')]);
      await pass(evening.add(const Duration(minutes: 45)));
      expect(surface.posts, hasLength(1));
      // ...and it is what the next evening's one notification is for.
      await pass(evening.add(const Duration(days: 1)));
      expect(surface.posts, ['4 memos waiting for triage', '5 memos waiting for triage']);
    });

    test('a full batch is a floor and says so', () async {
      server = batchOf(memos(25));
      await pass(evening);
      expect(surface.posts, ['25+ memos waiting for triage']);
    });

    test('one memo is singular', () async {
      server = batchOf(memos(1));
      await pass(evening);
      expect(surface.posts, ['1 memo waiting for triage']);
    });
  });

  group('nothing for a memo already triaged on the web', () {
    test('a batch emptied on the web before the evening never notifies', () async {
      server = batchOf(memos(9));
      await pass(DateTime(2026, 10, 5, 14));
      server = batchOf([]);
      await pass(evening);
      expect(surface.posts, isEmpty);
    });

    test('a row that already carries a decision is not counted', () async {
      server = batchOf([item('a', link: decided()), item('b', link: decided()), item('c')]);
      await pass(evening);
      expect(surface.posts, ['1 memo waiting for triage']);
    });

    test('a batch of only decided rows posts nothing', () async {
      server = batchOf([item('a', link: decided())]);
      await pass(evening);
      expect(surface.posts, isEmpty);
    });

    test('a memo the Scribe has not routed yet is not "ready"', () async {
      server = batchOf([item('a', status: 'absent', withProposal: false)]);
      await pass(evening);
      expect(surface.posts, isEmpty);
    });

    test('a posted notification is withdrawn when the last memo is decided elsewhere', () async {
      server = batchOf(memos(2));
      await pass(evening);
      server = batchOf([]);
      await pass(evening.add(const Duration(minutes: 15)));
      expect(surface.visible, isFalse);
      expect(surface.cancels, 1);
    });

    test('a notification still showing is corrected in place, silently', () async {
      server = batchOf(memos(9));
      await pass(evening);
      server = batchOf(memos(4));
      await pass(evening.add(const Duration(minutes: 15)));
      expect(surface.posts, hasLength(1));
      expect(surface.updates, ['4 memos waiting for triage']);
    });

    test('a dismissed notification is never brought back by a correction', () async {
      server = batchOf(memos(9));
      await pass(evening);
      surface.visible = false;
      server = batchOf(memos(4));
      await pass(evening.add(const Duration(minutes: 15)));
      expect(surface.updates, isEmpty);
    });
  });

  group('a silent zero is never mistaken for an answer', () {
    test('a failed read throws, posts nothing and remembers nothing', () async {
      await expectLater(
        runNudgePass(
          fetch: () async => throw gen.ApiException(503, 'triage is not configured'),
          surface: surface,
          prefs: prefs,
          now: evening,
        ),
        throwsA(isA<gen.ApiException>()),
      );
      expect(surface.posts, isEmpty);
      expect(surface.cancels, 0);
      expect(prefs.getKeys(), isEmpty);
    });

    test('a failed read does not withdraw a notification that is showing', () async {
      server = batchOf(memos(2));
      await pass(evening);
      await expectLater(
        runNudgePass(
          fetch: () async => throw gen.ApiException(401, 'unauthorized'),
          surface: surface,
          prefs: prefs,
          now: evening.add(const Duration(minutes: 15)),
        ),
        throwsA(anything),
      );
      expect(surface.visible, isTrue);
    });

    test('notifications switched off is not remembered as a nudge delivered', () async {
      server = batchOf(memos(3));
      surface.enabled = false;
      await pass(evening);
      surface.enabled = true;
      await pass(evening.add(const Duration(minutes: 15)));
      expect(surface.posts, ['3 memos waiting for triage']);
    });
  });
}
