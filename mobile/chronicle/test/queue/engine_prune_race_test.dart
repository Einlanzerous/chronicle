/// CHRN-120 criterion 6: a prune landing between `_attemptOne`'s
/// `sendable.length()` check and its later `readAsBytes()` must not throw out
/// of a concurrent `drainPass`.
///
/// This is a real race, not a hypothetical one, and CHRN-120 is what made it
/// one. Before it, nothing deleted an audio file, so the worst two engines
/// working the same capture could do was waste a request
/// (`engine_concurrent_test.dart`). The prune pass in the OTHER isolate deletes
/// the sendable file of an acknowledged capture; this isolate may be mid-attempt
/// on a snapshot taken before that ack, having already passed its length check.
/// `readAsBytes()` then threw an uncaught `FileSystemException` out of
/// `_attemptOne`, and `drainPass` has no catch around it.
///
/// The interleaving is forced deterministically -- the file is removed inside
/// the `openUpload` call, which is exactly the gap between the two reads --
/// rather than trusted to the event loop.
library;

import 'dart:io';

import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/capture_fixture.dart';
import 'support/fake_chronicle_server.dart';

late Directory _root;

void main() {
  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn120-race');
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  final now = DateTime(2026, 9, 25, 12);

  test('a prune landing after the length check does not throw out of drainPass, and '
      'never overwrites the acknowledged record the other isolate wrote', () async {
    final server = FakeChronicleServer();
    final capture = await writeFixtureCapture(
      _root,
      id: 'a',
      bytes: List.generate(24, (i) => i),
    );
    final transport = FakeUploadTransport(server);
    final engine = QueueEngine(transport: transport, chunkSize: 8, now: () => now);

    // What the other isolate does between this attempt's length check and its
    // read: acknowledge, then (much later, in reality -- compressed here) prune.
    transport.onBeforeCall = (call, {required isOpen}) async {
      if (isOpen) {
        await capture.queueDir.write(QueueRecord(
          status: QueueStatus.acknowledged,
          enqueuedAt: now,
          memoId: 'memo-from-the-other-isolate',
          acknowledgedAt: now,
          locallyPrunedAt: now,
        ));
        await capture.queueDir.capture.deleteSendable(CaptureState.ready);
      }
      return null;
    };

    final block = await engine.drainPass(
      captures: [capture],
      token: 'tok',
      serverUrl: 'https://chronicle-direct.example.com',
      tokenDigest: 'digest',
    );

    expect(block, isNull, reason: 'no device block: this was a per-capture matter');
    final record = (await capture.queueDir.read())!;
    expect(record.status, QueueStatus.acknowledged,
        reason: 'the loser of the race must not un-acknowledge a delivered capture');
    expect(record.memoId, 'memo-from-the-other-isolate');
    expect(record.locallyPrunedAt, now);
    expect(transport.calls, 1, reason: 'the attempt stopped at the vanished file');
    expect(server.memos, isEmpty, reason: 'and sent nothing');
  });

  test('a file that vanishes for any other reason is read as "the file changed", '
      'never as an uncaught exception', () async {
    final server = FakeChronicleServer();
    final capture = await writeFixtureCapture(
      _root,
      id: 'a',
      bytes: List.generate(24, (i) => i),
    );
    final transport = FakeUploadTransport(server);
    final engine = QueueEngine(transport: transport, chunkSize: 8, now: () => now);

    transport.onBeforeCall = (call, {required isOpen}) async {
      if (isOpen) await capture.queueDir.capture.audio.delete();
      return null;
    };

    await engine.drainPass(
      captures: [capture],
      token: 'tok',
      serverUrl: 'https://chronicle-direct.example.com',
      tokenDigest: 'digest',
    );

    expect((await capture.queueDir.read())!.status, QueueStatus.blockedLocalFileChanged);
    expect(server.memos, isEmpty);
  });
}
