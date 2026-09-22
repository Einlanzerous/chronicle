/// Criterion 11: two engines draining the SAME capture with the
/// foreground/background exclusion (`IsolateNameServer`) disabled. The
/// plan's own words for the failure mode this must stay inside: "two
/// engines produce a 409-offset resync and an idempotent complete, never a
/// second memo or an unverified ack."
///
/// The interleaving is forced deterministically with a [Completer] rather
/// than trusted to the event loop: engine A is held just before its SECOND
/// chunk until engine B has driven the same capture to completion. That is
/// exactly the shape of the race the exclusion exists to make rare --
/// A opens and sends one chunk, B (unaware) opens the same session,
/// resumes from A's already-landed offset, and finishes it -- and it
/// proves the interesting path: A's own stale chunk arrives AFTER the
/// memo already exists.
library;

import 'dart:async';
import 'dart:io';

import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/capture_fixture.dart';
import 'support/fake_chronicle_server.dart';

late Directory _root;

void main() {
  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn61-race');
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  test(
    'A opens and sends chunk 1, B completes the whole upload, A\'s late chunk 2 '
    'resyncs instead of duplicating -- one memo, one final ack',
    () async {
      final server = FakeChronicleServer();
      final capture = await writeFixtureCapture(
        _root,
        id: 'a',
        bytes: List.generate(24, (i) => i), // three 8-byte chunks
      );

      final transportA = FakeUploadTransport(server);
      final transportB = FakeUploadTransport(server);
      final now = DateTime(2026, 1, 1, 12);
      final engineA = QueueEngine(transport: transportA, chunkSize: 8, now: () => now);
      final engineB = QueueEngine(transport: transportB, chunkSize: 8, now: () => now);

      final bDone = Completer<void>();
      // A's own third call is its SECOND chunk (1=open, 2=chunk1, 3=chunk2).
      // Held until B has entirely finished.
      transportA.onBeforeCall = (call, {required isOpen}) async {
        if (!isOpen && call == 3) {
          await bDone.future;
        }
        return null;
      };

      final aFuture = engineA.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      await engineB.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );
      bDone.complete();
      await aFuture;

      expect(server.memos, hasLength(1), reason: 'never a second memo');
      final record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.acknowledged, reason: 'never lost, once genuinely acked');
      expect(record.memoId, server.memos.single.id);
    },
  );
}
