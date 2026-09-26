import 'dart:io';

import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:flutter_test/flutter_test.dart';

late Directory _root;

void main() {
  setUp(() async {
    _root = await Directory.systemTemp.createTemp('chrn61-record');
  });

  tearDown(() async {
    if (await _root.exists()) await _root.delete(recursive: true);
  });

  QueueDir dir(String id) => QueueDir(CaptureDir(_root, id));

  test('a capture with no upload.json reads as absent, not as an error', () async {
    expect(await dir('a').read(), isNull);
  });

  test('write then read round-trips every field', () async {
    final now = DateTime.now();
    final record = QueueRecord(
      status: QueueStatus.rejected,
      enqueuedAt: now,
      attemptCount: 4,
      lastAttemptAt: now.add(const Duration(minutes: 1)),
      lastFailureClass: FailureClass.rejectedOther,
      lastFailureCode: '400',
      failureStreak: 3,
      rejectReason: RejectReason.serverRefused,
    );
    final d = dir('b');
    await d.write(record);
    final read = await d.read();

    expect(read, isNotNull);
    expect(read!.status, QueueStatus.rejected);
    expect(read.enqueuedAt, now);
    expect(read.attemptCount, 4);
    expect(read.lastAttemptAt, now.add(const Duration(minutes: 1)));
    expect(read.lastFailureClass, FailureClass.rejectedOther);
    expect(read.lastFailureCode, '400');
    expect(read.failureStreak, 3);
    expect(read.rejectReason, RejectReason.serverRefused);
  });

  test('an acknowledged record carries its memo id and timestamp', () async {
    final now = DateTime.now();
    final record = QueueRecord.fresh(now).copyWith(
      status: QueueStatus.acknowledged,
      memoId: 'memo-123',
      acknowledgedAt: now.add(const Duration(seconds: 5)),
    );
    final d = dir('c');
    await d.write(record);
    final read = await d.read();

    expect(read!.status, QueueStatus.acknowledged);
    expect(read.isTerminal, isTrue);
    expect(read.memoId, 'memo-123');
  });

  test('a corrupt upload.json reads as absent, exactly like a missing one', () async {
    final d = dir('d');
    await d.capture.dir.create(recursive: true);
    await d.uploadFile.writeAsString('{not json');
    expect(await d.read(), isNull);
  });

  test('an unrecognised status string falls back to pending rather than throwing', () async {
    final d = dir('e');
    await d.capture.dir.create(recursive: true);
    await d.uploadFile.writeAsString(
      '{"status":"some_future_status","enqueued_at":"${DateTime.now().toIso8601String()}"}',
    );
    final read = await d.read();
    expect(read!.status, QueueStatus.pending);
  });

  test('a write is atomic: no partial file is ever visible mid-write', () async {
    final d = dir('f');
    await d.write(QueueRecord.fresh(DateTime.now()));
    // Only the final file should exist, never a .tmp left behind.
    final siblings = await d.capture.dir.list().toList();
    expect(siblings.map((e) => e.path.split('/').last).toList(), ['upload.json']);
  });

  test('copyWith leaves fields not passed unchanged, and clears only what is asked', () {
    final base = QueueRecord.fresh(DateTime.now()).copyWith(
      lastFailureClass: FailureClass.network,
      lastFailureCode: 'net',
    );
    final untouched = base.copyWith(attemptCount: 1);
    expect(untouched.lastFailureClass, FailureClass.network);
    expect(untouched.lastFailureCode, 'net');

    final cleared =
        base.copyWith(clearLastFailureClass: true, clearLastFailureCode: true);
    expect(cleared.lastFailureClass, isNull);
    expect(cleared.lastFailureCode, isNull);
  });

  test('the queue never touches audio, trimmed or meta -- only upload.json', () async {
    final capture = CaptureDir(_root, 'g');
    await capture.dir.create(recursive: true);
    await capture.audio.writeAsBytes([1, 2, 3], flush: true);
    final metaRecord = CaptureRecord(
      captureId: 'g',
      idempotencyKey: 'chr-cap-g',
      startedAt: DateTime.now(),
      state: CaptureState.ready,
      contentHash: 'abc',
      byteSize: 3,
    );
    await capture.writeMeta(metaRecord);

    final before = await capture.audio.readAsBytes();
    final metaBefore = await capture.meta.readAsString();

    await QueueDir(capture).write(QueueRecord.fresh(DateTime.now()));

    expect(await capture.audio.readAsBytes(), before);
    expect(await capture.meta.readAsString(), metaBefore);
  });

  group('CHRN-120: locallyPrunedAt and lastPolledAt', () {
    test('both survive a write and a read', () async {
      final now = DateTime(2026, 9, 25, 12);
      final record = QueueRecord.fresh(now).copyWith(
        status: QueueStatus.acknowledged,
        memoId: 'memo-1',
        acknowledgedAt: now,
        locallyPrunedAt: now.add(const Duration(days: 30)),
        lastPolledAt: now.add(const Duration(days: 29)),
      );
      final d = dir('p');
      await d.write(record);

      final read = (await d.read())!;

      expect(read.locallyPrunedAt, now.add(const Duration(days: 30)));
      expect(read.lastPolledAt, now.add(const Duration(days: 29)));
    });

    test('a record written before this ticket reads with both absent -- "not '
        'pruned" and "never polled", the correct reading of each', () async {
      final d = dir('old');
      await d.capture.dir.create(recursive: true);
      await d.uploadFile.writeAsString('''
{
  "status": "acknowledged",
  "enqueued_at": "2026-09-01T08:00:00.000",
  "attempt_count": 1,
  "failure_streak": 0,
  "memo_id": "memo-old",
  "acknowledged_at": "2026-09-01T08:01:00.000"
}''');

      final read = (await d.read())!;

      expect(read.status, QueueStatus.acknowledged);
      expect(read.memoId, 'memo-old');
      expect(read.locallyPrunedAt, isNull);
      expect(read.lastPolledAt, isNull);
    });

    test('an unparseable timestamp reads as absent rather than throwing', () {
      final read = QueueRecord.fromJson({
        'status': 'acknowledged',
        'enqueued_at': '2026-09-01T08:00:00.000',
        'locally_pruned_at': 'not a date',
        'last_polled_at': 'nor this',
      });

      expect(read.locallyPrunedAt, isNull);
      expect(read.lastPolledAt, isNull);
    });

    test('nothing the engine does to a record can clear either field', () {
      final now = DateTime(2026, 9, 25);
      final marked = QueueRecord.fresh(now).copyWith(
        locallyPrunedAt: now,
        lastPolledAt: now,
      );

      // Every copyWith the engine makes -- an ack, a failure, a retry -- passes
      // neither field, so both must come through untouched. There is no `clear`
      // flag for either: locallyPrunedAt is set once and never cleared, the
      // same rule acknowledgedAt follows.
      final after = marked.copyWith(
        status: QueueStatus.pending,
        attemptCount: 9,
        clearLastAttemptAt: true,
        clearLastFailureClass: true,
        clearLastFailureCode: true,
        clearRejectReason: true,
      );

      expect(after.locallyPrunedAt, now);
      expect(after.lastPolledAt, now);
    });
  });
}
