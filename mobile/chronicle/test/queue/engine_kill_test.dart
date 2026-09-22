/// The kill harness (criterion 5): interrupt an upload at every I/O
/// boundary of a multi-chunk send, then hand the SAME on-disk capture to a
/// FRESH [QueueEngine] instance and keep draining. Proves the invariant by
/// construction rather than by inspection: [QueueDir] persists no
/// `uploading` state (build step 1's own rule), so "kill" here just means
/// "stop calling the engine" -- there is no half-written state for a fresh
/// instance to inherit or misread.
///
/// At every boundary this checks the one property that matters:
/// **acknowledged implies the server holds that memo, with a verified
/// hash and size, and never more than once.**
library;

import 'dart:io';

import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/capture_fixture.dart';
import 'support/fake_chronicle_server.dart';

late Directory _root;

void main() {
  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn61-kill');
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  /// The number of transport calls a clean, uninterrupted 3-chunk upload
  /// makes: one open, three appends.
  const totalCalls = 4;

  for (var killAt = 1; killAt <= totalCalls; killAt++) {
    for (final afterSend in [false, true]) {
      if (afterSend && killAt == 1) {
        // "After the server applied it" makes no sense for the open call
        // in a way this harness needs to distinguish from `beforeSend` --
        // opening is idempotent by key either way. Skip the redundant case.
        continue;
      }
      test(
        'killed at call $killAt (${afterSend ? "after" : "before"} the server sees it) '
        '-- a fresh engine converges to exactly one verified memo',
        () async {
          final server = FakeChronicleServer();
          final transport = FakeUploadTransport(server);
          var clock = DateTime(2026, 1, 1, 12);
          final bytes = List.generate(24, (i) => (i * 7) % 256); // 3 chunks of 8
          final capture = await writeFixtureCapture(
            _root,
            id: 'a',
            bytes: bytes,
            startedAt: clock,
          );

          transport.onBeforeCall = (call, {required isOpen}) {
            if (call != killAt) return null;
            final error = const SocketException('connection reset');
            return afterSend
                ? TransportFault.afterSend(error)
                : TransportFault.beforeSend(error);
          };

          final engine = QueueEngine(transport: transport, now: () => clock, chunkSize: 8);
          await engine.drainPass(
            captures: [capture],
            token: 'tok',
            serverUrl: 'https://chronicle-direct.example.com',
            tokenDigest: 'digest',
          );

          // The invariant, checked at the exact instant the "kill" landed:
          // never acknowledged without the server actually holding a
          // matching memo.
          final afterKill = await capture.queueDir.read();
          if (afterKill?.status == QueueStatus.acknowledged) {
            final memo = server.memos.singleWhere((m) => m.id == afterKill!.memoId);
            expect(memo.contentHash, capture.capture.contentHash);
            expect(memo.byteSize, capture.capture.byteSize);
          }
          expect(server.memos.length, lessThanOrEqualTo(1));

          // Now simulate the relaunch: a BRAND NEW engine, no fault
          // injection, same directory, same server. Drive it to
          // convergence.
          transport.onBeforeCall = null;
          var current = await reloadCapture(capture);
          final freshEngine = QueueEngine(transport: transport, now: () => clock, chunkSize: 8);
          for (var pass = 0; pass < 5 && current.queueRecord.status != QueueStatus.acknowledged; pass++) {
            clock = clock.add(const Duration(minutes: 20)); // clear of any backoff
            await freshEngine.drainPass(
              captures: [current],
              token: 'tok',
              serverUrl: 'https://chronicle-direct.example.com',
              tokenDigest: 'digest',
            );
            current = await reloadCapture(current);
          }

          expect(current.queueRecord.status, QueueStatus.acknowledged,
              reason: 'kill at call $killAt (afterSend=$afterSend) never converged');
          expect(server.memos, hasLength(1), reason: 'never more than one memo for one capture');
          final memo = server.memos.single;
          expect(memo.contentHash, capture.capture.contentHash);
          expect(memo.byteSize, capture.capture.byteSize);
          expect(current.queueRecord.memoId, memo.id);
        },
      );
    }
  }
}
