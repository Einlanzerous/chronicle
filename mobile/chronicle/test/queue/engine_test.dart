import 'dart:io';

import 'package:chronicle/api/transport.dart';
import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/queue/device_block.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:chronicle/queue/upload_transport.dart';
import 'package:chronicle_api/api.dart';
import 'package:flutter_test/flutter_test.dart';

import 'support/capture_fixture.dart';
import 'support/fake_chronicle_server.dart';

late Directory _root;

void main() {
  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn61-engine');
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  group('the happy path', () {
    test('open, chunks, ack -- one capture, forced into several chunks', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport, chunkSize: 4);
      final capture = await writeFixtureCapture(
        _root,
        id: 'a',
        bytes: List.generate(37, (i) => i % 256),
      );

      final block = await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(block, isNull);
      expect(server.memos, hasLength(1));
      final record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.acknowledged);
      expect(record.memoId, server.memos.single.id);
      expect(record.acknowledgedAt, isNotNull);
      // CHRN-118: the capture's own startedAt reaches the memo as
      // recorded_at, unchanged -- display-only, never this client's own
      // retention clock (QueueRecord.enqueuedAt's doc comment already says
      // so).
      expect(server.memos.single.recordedAt, capture.capture.startedAt);
    });

    test('ten captures drain sequentially, oldest startedAt first, all acknowledged', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport, chunkSize: 8);

      final captures = <QueueCapture>[];
      for (var i = 9; i >= 0; i--) {
        // Written in REVERSE id order, so "oldest first" is a real sorting
        // claim, not an accident of list order. Unique bytes per capture so
        // a committed memo can be traced back to the capture it came from.
        captures.add(await writeFixtureCapture(
          _root,
          id: 'c$i',
          bytes: List.generate(20, (b) => (b + i) % 256),
          startedAt: DateTime(2026, 1, 1).add(Duration(minutes: i)),
        ));
      }

      final block = await engine.drainPass(
        captures: captures,
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(block, isNull);
      expect(server.memos, hasLength(10));
      // One capture is driven to completion before the next is even
      // opened, so the server's own commit order IS the pass's attempt
      // order -- and it must be oldest `startedAt` first: c0 .. c9.
      final commitOrder = server.memos.map((m) => _captureIdForHash(captures, m)).toList();
      expect(commitOrder, ['c0', 'c1', 'c2', 'c3', 'c4', 'c5', 'c6', 'c7', 'c8', 'c9']);
      for (final c in captures) {
        final record = await c.queueDir.read();
        expect(record!.status, QueueStatus.acknowledged, reason: c.capture.captureId);
      }
    });

    test("the queue's write surface never touches the sendable audio bytes", () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport, chunkSize: 6);
      final bytes = List.generate(29, (i) => i * 3 % 256);
      final capture = await writeFixtureCapture(_root, id: 'a', bytes: bytes);
      final before = await capture.queueDir.capture.audio.readAsBytes();

      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      final after = await capture.queueDir.capture.audio.readAsBytes();
      expect(after, equals(before));
    });
  });

  group('criterion 1: no network at all ends the pass; restoring drains everything', () {
    test('airplane mode: the first capture records the failure, the rest are never touched',
        () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      transport.onBeforeCall = (call, {required isOpen}) =>
          const TransportFault.beforeSend(SocketException('network is unreachable'));
      final engine = QueueEngine(transport: transport, now: () => DateTime(2026, 1, 1, 12));

      final captures = [
        for (var i = 0; i < 10; i++)
          await writeFixtureCapture(
            _root,
            id: 'a$i',
            bytes: List.generate(10, (b) => b),
            startedAt: DateTime(2026, 1, 1).add(Duration(minutes: i)),
          ),
      ];

      final block = await engine.drainPass(
        captures: captures,
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(block, isNull); // a network PassEnds is not a device block
      expect(transport.calls, 1, reason: 'the pass stops at the first refusal');
      expect(server.memos, isEmpty);

      final first = await captures.first.queueDir.read();
      expect(first!.status, QueueStatus.pending);
      expect(first.attemptCount, 1);
      expect(first.lastFailureClass, FailureClass.network);

      for (final c in captures.skip(1)) {
        expect(await c.queueDir.read(), isNull, reason: '${c.capture.captureId} was never reached');
      }
    });

    test('restored connectivity drains all ten in the next pass', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      var clock = DateTime(2026, 1, 1, 12);
      final engine = QueueEngine(transport: transport, now: () => clock, chunkSize: 8);

      var captures = [
        for (var i = 0; i < 10; i++)
          await writeFixtureCapture(
            _root,
            id: 'a$i',
            bytes: List.generate(12, (b) => b + i),
            startedAt: DateTime(2026, 1, 1).add(Duration(minutes: i)),
          ),
      ];

      transport.onBeforeCall = (call, {required isOpen}) =>
          const TransportFault.beforeSend(SocketException('network is unreachable'));
      await engine.drainPass(
        captures: captures,
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      transport.onBeforeCall = null;
      clock = clock.add(const Duration(minutes: 20)); // clear of any backoff
      captures = [for (final c in captures) await reloadCapture(c)];
      final block = await engine.drainPass(
        captures: captures,
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(block, isNull);
      expect(server.memos, hasLength(10));
      expect(server.memos.map((m) => m.contentHash).toSet(), hasLength(10));
      for (final c in captures) {
        final record = await c.queueDir.read();
        expect(record!.status, QueueStatus.acknowledged, reason: c.capture.captureId);
      }
    });
  });

  group('criterion 4: a lost response after the server commits resolves via re-open', () {
    test('raw exception on the final chunk\'s response -- next pass acks, server has one memo',
        () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      var clock = DateTime(2026, 1, 1, 12);
      final engine = QueueEngine(transport: transport, now: () => clock, chunkSize: 8);
      final capture = await writeFixtureCapture(
        _root,
        id: 'a',
        bytes: List.generate(16, (i) => i), // exactly two 8-byte chunks
      );

      // Fail AFTER the server applies the second (final) append -- a raw,
      // unwrapped exception, exactly the shape `api_client.dart`'s own
      // un-awaited `Response.fromStream` can leak. Call 1 is the open,
      // call 2 the first chunk, call 3 the final one.
      transport.onBeforeCall = (call, {required isOpen}) {
        if (!isOpen && call == 3) {
          return const TransportFault.afterSend(SocketException('connection reset'));
        }
        return null;
      };

      final firstPassBlock = await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );
      expect(firstPassBlock, isNull);
      expect(server.memos, hasLength(1), reason: 'the server DID commit the memo');
      var record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.pending, reason: 'the client never saw an ack');
      expect(record.lastFailureClass, FailureClass.network);

      transport.onBeforeCall = null;
      clock = clock.add(const Duration(minutes: 20));
      final reloaded = await reloadCapture(capture);
      final secondPassBlock = await engine.drainPass(
        captures: [reloaded],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(secondPassBlock, isNull);
      expect(server.memos, hasLength(1), reason: 'the re-open replayed, it did not double-send');
      record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.acknowledged);
      expect(record.memoId, server.memos.single.id);
    });
  });

  group('device-level refusals: never recorded against the capture that surfaced them', () {
    test('401 blocks the whole pass; the capture is left untouched', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      transport.onBeforeCall = (call, {required isOpen}) =>
          TransportFault.beforeSend(ApiException(401, '{"code":"unauthorized"}'));
      final engine = QueueEngine(transport: transport);
      final capture = await writeFixtureCapture(_root, id: 'a', bytes: [1, 2, 3]);

      final block = await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(block, isA<DeviceBlock>());
      expect(block!.reason, DeviceBlockReason.signedOut);
      expect(await capture.queueDir.read(), isNull);
    });

    test('no token held blocks the pass before any request is made', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport);
      final capture = await writeFixtureCapture(_root, id: 'a', bytes: [1, 2, 3]);

      final block = await engine.drainPass(
        captures: [capture],
        token: null,
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: '',
      );

      expect(block!.reason, DeviceBlockReason.signedOut);
      expect(transport.calls, 0);
    });

    test('a still-applying block ends the pass with zero requests', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport);
      final capture = await writeFixtureCapture(_root, id: 'a', bytes: [1, 2, 3]);
      const block = DeviceBlock(
        reason: DeviceBlockReason.signedOut,
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      final result = await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
        currentBlock: block,
      );

      expect(identical(result, block), isTrue);
      expect(transport.calls, 0);
    });

    test('a block whose inputs changed no longer applies -- the pass proceeds', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport, chunkSize: 8);
      final capture = await writeFixtureCapture(_root, id: 'a', bytes: List.generate(8, (i) => i));
      const stale = DeviceBlock(
        reason: DeviceBlockReason.signedOut,
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'old-digest',
      );

      final result = await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'new-digest',
        currentBlock: stale,
      );

      expect(result, isNull);
      expect(server.memos, hasLength(1));
    });

    test('NotChronicleException (Access-gated host) is a wrongHost block, not a rejection',
        () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      transport.onBeforeCall = (call, {required isOpen}) => TransportFault.beforeSend(
            NotChronicleException(
              statusCode: 302,
              location: 'https://chronicle.example.com/cdn-cgi/access/login',
              requestedUrl: Uri.parse('https://chronicle.example.com/memos/uploads'),
            ),
          );
      final engine = QueueEngine(transport: transport);
      final capture = await writeFixtureCapture(_root, id: 'a', bytes: [1, 2, 3]);

      final block = await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle.example.com',
        tokenDigest: 'digest',
      );

      expect(block!.reason, DeviceBlockReason.wrongHost);
      expect(await capture.queueDir.read(), isNull);
    });
  });

  group('rejections parked on the first occurrence -- never a matter of retry count', () {
    test('idempotency_key_reused parks immediately', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport, chunkSize: 8);

      final first = await writeFixtureCapture(_root, id: 'a', bytes: List.generate(8, (i) => i));
      await engine.drainPass(
        captures: [first],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      // A second capture minted with the SAME idempotency key (the bug this
      // guards against would be a broken key-derivation, not something the
      // engine causes) but DIFFERENT content.
      final captureDir = CaptureDir(_root, 'b');
      final colliding = CaptureRecord(
        captureId: 'b',
        idempotencyKey: first.capture.idempotencyKey,
        startedAt: DateTime(2026, 1, 2),
        state: CaptureState.ready,
        contentHash: 'different-hash-entirely',
        byteSize: 4,
      );
      await captureDir.writeMeta(colliding);
      await captureDir.audio.writeAsBytes([9, 9, 9, 9], flush: true);
      final second = QueueCapture(
        queueDir: QueueDir(captureDir),
        capture: colliding,
        queueRecord: QueueRecord.fresh(colliding.startedAt),
      );

      await engine.drainPass(
        captures: [second],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      final record = await second.queueDir.read();
      expect(record!.status, QueueStatus.rejected);
      expect(record.rejectReason, RejectReason.keyReused);
      expect(record.attemptCount, 1, reason: 'parked on the FIRST occurrence');
    });

    test('content_hash_mismatch parks immediately', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport, chunkSize: 8);
      final bytes = List.generate(8, (i) => i);
      // Declares a hash that does not actually belong to these bytes.
      final capture = await writeFixtureCapture(
        _root,
        id: 'a',
        bytes: bytes,
        contentHashOverride: 'not-the-real-hash',
      );

      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      final record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.rejected);
      expect(record.rejectReason, RejectReason.hashMismatch);
      expect(record.attemptCount, 1);
      expect(server.memos, isEmpty);
    });
  });

  group('escalation: only after maxConsecutiveFailures, and only for the classes that ever do',
      () {
    test('a real 4xx parks after three consecutive occurrences, not sooner', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      transport.onBeforeCall = (call, {required isOpen}) =>
          TransportFault.beforeSend(ApiException(400, '{"code":"weird_request"}'));
      var clock = DateTime(2026, 1, 1, 12);
      final engine = QueueEngine(transport: transport, now: () => clock, maxConsecutiveFailures: 3);
      var capture = await writeFixtureCapture(_root, id: 'a', bytes: [1, 2, 3]);

      for (var attempt = 1; attempt <= 2; attempt++) {
        await engine.drainPass(
          captures: [capture],
          token: 'tok',
          serverUrl: 'https://chronicle-direct.example.com',
          tokenDigest: 'digest',
        );
        final record = await capture.queueDir.read();
        expect(record!.status, QueueStatus.pending, reason: 'attempt $attempt');
        expect(record.failureStreak, attempt);
        capture = await reloadCapture(capture);
        clock = clock.add(const Duration(minutes: 20));
      }

      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );
      final record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.rejected);
      expect(record.rejectReason, RejectReason.serverRefused);
      expect(record.failureStreak, 3);
    });

    test('a transient 5xx never escalates, however many times it repeats', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      transport.onBeforeCall = (call, {required isOpen}) =>
          TransportFault.beforeSend(ApiException(503, '{"code":"unavailable"}'));
      var clock = DateTime(2026, 1, 1, 12);
      final engine = QueueEngine(transport: transport, now: () => clock);
      var capture = await writeFixtureCapture(_root, id: 'a', bytes: [1, 2, 3]);

      for (var attempt = 1; attempt <= 6; attempt++) {
        await engine.drainPass(
          captures: [capture],
          token: 'tok',
          serverUrl: 'https://chronicle-direct.example.com',
          tokenDigest: 'digest',
        );
        final record = await capture.queueDir.read();
        expect(record!.status, QueueStatus.pending, reason: 'attempt $attempt');
        expect(record.failureStreak, attempt);
        capture = await reloadCapture(capture);
        clock = clock.add(const Duration(minutes: 20));
      }
    });

    test('a repeating no-progress resume (ErrStagingLost-shaped) parks after three, not sooner',
        () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      transport.onBeforeCall = (call, {required isOpen}) {
        if (isOpen) return null;
        return TransportFault.beforeSend(ApiException(
          409,
          '{"status":"incomplete","upload_id":"whatever","byte_size":8,"offset":0,"duplicate":false}',
        ));
      };
      var clock = DateTime(2026, 1, 1, 12);
      final engine = QueueEngine(transport: transport, now: () => clock, chunkSize: 8);
      var capture = await writeFixtureCapture(_root, id: 'a', bytes: List.generate(8, (i) => i));

      for (var attempt = 1; attempt <= 2; attempt++) {
        await engine.drainPass(
          captures: [capture],
          token: 'tok',
          serverUrl: 'https://chronicle-direct.example.com',
          tokenDigest: 'digest',
        );
        final record = await capture.queueDir.read();
        expect(record!.status, QueueStatus.pending, reason: 'attempt $attempt');
        expect(record.lastFailureClass, FailureClass.noProgress);
        capture = await reloadCapture(capture);
        clock = clock.add(const Duration(minutes: 20));
      }

      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );
      final record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.rejected);
      expect(record.rejectReason, RejectReason.protocol);
    });
  });

  group('preconditions the engine checks before ever touching the transport', () {
    test('a file whose length no longer matches its declaration is blocked, not sent', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport);
      final capture = await writeFixtureCapture(_root, id: 'a', bytes: [1, 2, 3, 4]);
      await capture.queueDir.capture.audio.writeAsBytes([1, 2], flush: true);

      final block = await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(block, isNull);
      expect(transport.calls, 0);
      final record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.blockedLocalFileChanged);
    });

    test('a recording or empty capture is not queue material -- skipped, not blocked', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final engine = QueueEngine(transport: transport);
      final capture = await writeFixtureCapture(
        _root,
        id: 'a',
        bytes: [1, 2, 3],
        state: CaptureState.recording,
      );

      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(transport.calls, 0);
      expect(await capture.queueDir.read(), isNull);
    });

    test('retention gate closed: enqueuedAt in the future skips this pass entirely', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      final now = DateTime(2026, 1, 1, 12);
      final engine = QueueEngine(transport: transport, now: () => now);
      final capture = await writeFixtureCapture(
        _root,
        id: 'a',
        bytes: [1, 2, 3],
        startedAt: now,
        // Strictly after "now": the shipped grace is zero, so only a
        // future enqueuedAt can close the gate -- proves the engine's
        // wiring without needing a non-zero grace, which
        // `retention_gate_test.dart` already covers at the unit level.
        enqueuedAt: now.add(const Duration(hours: 1)),
      );

      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      expect(transport.calls, 0);
      expect(await capture.queueDir.read(), isNull);
    });

    test('backoff not yet elapsed skips a retry; elapsing it lets the retry through', () async {
      final server = FakeChronicleServer();
      final transport = FakeUploadTransport(server);
      transport.onBeforeCall = (call, {required isOpen}) =>
          TransportFault.beforeSend(ApiException(503, '{"code":"unavailable"}'));
      var clock = DateTime(2026, 1, 1, 12);
      final engine = QueueEngine(transport: transport, now: () => clock, chunkSize: 8);
      var capture = await writeFixtureCapture(_root, id: 'a', bytes: List.generate(8, (i) => i));

      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );
      expect(transport.calls, 1);
      capture = await reloadCapture(capture);

      // Same instant: still backed off, no new call.
      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );
      expect(transport.calls, 1);

      transport.onBeforeCall = null;
      clock = clock.add(const Duration(seconds: 30)); // past the 15s+jitter initial delay
      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );
      expect(transport.calls, greaterThan(1));
      final record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.acknowledged);
    });
  });

  group('an unverified 2xx is never an ack', () {
    test('the server answers complete but the memo hash does not match the capture\'s own',
        () async {
      // A hand-crafted transport standing in for a server bug or a stale
      // duplicate answer: `complete`, but the memo attached does not belong
      // to this capture. `verifyAck` must refuse it.
      final capture = await writeFixtureCapture(_root, id: 'a', bytes: [1, 2, 3]);
      final wrongMemo = Memo(
        id: 'someone-elses-memo',
        state: 'captured',
        retention: MemoRetentionEnum.days30,
        contentHash: 'not-this-captures-hash',
        byteSize: 999,
        capturedAt: DateTime.now().toUtc(),
        recordedAt: null,
        audioPruned: false,
        retentionStatus: 'scheduled',
        prunesAt: null,
        audioPrunedAt: null,
        durationMs: 0,
        codec: 'opus',
        sampleRateHz: 48000,
      );
      final transport = _StaticTransport(UploadState(
        status: UploadStateStatusEnum.complete,
        uploadId: 'up-1',
        byteSize: 3,
        offset: 3,
        memo: wrongMemo,
        duplicate: false,
      ));
      final engine = QueueEngine(transport: transport);

      await engine.drainPass(
        captures: [capture],
        token: 'tok',
        serverUrl: 'https://chronicle-direct.example.com',
        tokenDigest: 'digest',
      );

      final record = await capture.queueDir.read();
      expect(record!.status, QueueStatus.pending, reason: 'never acknowledged on a mismatch');
      expect(record.memoId, isNull);
    });
  });
}

/// Finds which fixture capture's declared hash matches a committed [Memo]
/// -- used only to read back commit order for the ordering assertion.
String _captureIdForHash(List<QueueCapture> captures, Memo memo) => captures
    .firstWhere((c) => c.capture.contentHash == memo.contentHash)
    .capture
    .captureId;

/// A transport that always answers [state], regardless of which call is
/// made -- for exercising `verifyAck`'s refusal path directly, without a
/// server behind it.
class _StaticTransport implements UploadTransport {
  _StaticTransport(this.state);
  final UploadState state;

  @override
  Future<UploadState> openUpload({
    required String idempotencyKey,
    required String contentHash,
    required int byteSize,
    required DateTime recordedAt,
    String? retention,
  }) async =>
      state;

  @override
  Future<UploadState> appendChunk({
    required String uploadId,
    required int offset,
    required List<int> bytes,
  }) async =>
      state;
}
