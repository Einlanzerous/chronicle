/// The WorkManager leg of the queue, through its own seam.
///
/// `background.dart` is the one place the queue runs with nobody watching, and
/// before CHRN-120 its whole scan/drain body was private to a function that also
/// reads secure storage and `SharedPreferences`, so none of it was tested. The
/// prune pass would have been a deletion path with an untested trigger.
/// `runBackgroundQueuePass` is that body with its dependencies passed in.
///
/// What is proved here is what only this leg can get wrong: that the prune pass
/// runs AFTER the drain and never before it, that it does not run at all in a
/// build that has not opted in or under a device block, and that this leg's scan
/// makes the same decision about a lost `upload.json` as the foreground's.
library;

import 'dart:io';

import 'package:chronicle/queue/background.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/capture_fixture.dart';
import 'support/fake_audio_gate.dart';
import 'support/fake_chronicle_server.dart';

late Directory _root;
final _now = DateTime(2026, 9, 25, 12);
const _url = 'https://chronicle-direct.example.com';

void main() {
  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn120-bg');
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  QueueEngine engineOver(FakeUploadTransport t) =>
      QueueEngine(transport: t, chunkSize: 8, now: () => _now);

  test('the prune pass runs after every send and never before one', () async {
    final events = <String>[];
    final server = FakeChronicleServer();
    final transport = FakeUploadTransport(server)
      ..onBeforeCall = (call, {required isOpen}) {
        events.add('send');
        return null;
      };
    // discard_now is eligible from its ack, so the SAME wake sends it and then
    // asks about it -- the strictest ordering this can be put to.
    final qc = await writeFixtureCapture(_root,
        id: 'a', bytes: List.generate(20, (i) => i), retention: 'discard_now');
    final gate = FakeAudioGate()
      ..fallback = Answers.pruned
      ..onProbe = (_) async => events.add('probe');

    await runBackgroundQueuePass(
      root: _root,
      token: 'tok',
      serverUrl: _url,
      engine: engineOver(transport),
      gate: gate,
      now: () => _now,
      pruneEnabled: true,
    );

    expect(server.memos, hasLength(1));
    expect(events.first, 'send');
    expect(events.indexOf('probe'), greaterThan(events.lastIndexOf('send')),
        reason: 'a capture is delivered before anything is ever deleted');
    expect(await qc.queueDir.capture.audio.exists(), isFalse);
    expect(await qc.queueDir.capture.pruned.exists(), isTrue);
    expect((await qc.queueDir.read())!.status, QueueStatus.acknowledged);
  });

  test('a build that has not opted in sends, and never asks about deleting', () async {
    final server = FakeChronicleServer();
    final qc = await writeFixtureCapture(_root,
        id: 'a', bytes: List.generate(20, (i) => i), retention: 'discard_now');
    final gate = FakeAudioGate()..fallback = Answers.pruned;

    await runBackgroundQueuePass(
      root: _root,
      token: 'tok',
      serverUrl: _url,
      engine: engineOver(FakeUploadTransport(server)),
      gate: gate,
      now: () => _now,
      // pruneEnabled omitted: the compile-time default, which is off.
    );

    expect(server.memos, hasLength(1), reason: 'the drain still runs');
    expect(gate.probed, isEmpty);
    expect(await qc.queueDir.capture.audio.exists(), isTrue);
  });

  test('a device block from the drain skips the prune pass', () async {
    final server = FakeChronicleServer();
    final qc = await writeAckedCapture(_root,
        id: 'a', bytes: List.generate(20, (i) => i), startedAt: DateTime(2026, 8, 1));
    final gate = FakeAudioGate()..fallback = Answers.pruned;

    await runBackgroundQueuePass(
      root: _root,
      token: '', // signed out: the drain reports a block
      serverUrl: _url,
      engine: engineOver(FakeUploadTransport(server)),
      gate: gate,
      now: () => _now,
      pruneEnabled: true,
    );

    expect(gate.probed, isEmpty);
    expect(await qc.queueDir.capture.audio.exists(), isTrue);
  });

  test('this leg\'s scan does not re-enqueue a pruned capture whose upload.json was '
      'lost', () async {
    final server = FakeChronicleServer();
    final transport = FakeUploadTransport(server);
    final qc = await writeAckedCapture(_root,
        id: 'a', bytes: List.generate(20, (i) => i), startedAt: DateTime(2026, 8, 1));
    await runBackgroundQueuePass(
      root: _root,
      token: 'tok',
      serverUrl: _url,
      engine: engineOver(transport),
      gate: FakeAudioGate({'memo-a': Answers.pruned}),
      now: () => _now,
      pruneEnabled: true,
    );
    expect(await qc.queueDir.capture.audio.exists(), isFalse);

    await qc.queueDir.uploadFile.delete();
    final callsBefore = transport.calls;
    await runBackgroundQueuePass(
      root: _root,
      token: 'tok',
      serverUrl: _url,
      engine: engineOver(transport),
      gate: FakeAudioGate({'memo-a': Answers.pruned}),
      now: () => _now,
      pruneEnabled: true,
    );

    final record = (await qc.queueDir.read())!;
    expect(record.status, QueueStatus.acknowledged,
        reason: 'rebuilt from the tombstone, never a fresh pending');
    expect(record.memoId, 'memo-a');
    expect(transport.calls, callsBefore, reason: 'and nothing was re-sent');
    expect(server.memos, isEmpty);
  });
}
