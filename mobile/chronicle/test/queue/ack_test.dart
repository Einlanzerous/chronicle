import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/queue/ack.dart';
import 'package:chronicle_api/api.dart';
import 'package:flutter_test/flutter_test.dart';

CaptureRecord _record({String? hash = 'abc123', int? size = 10}) => CaptureRecord(
      captureId: 'cap-1',
      idempotencyKey: 'chr-cap-cap-1',
      startedAt: DateTime.now(),
      state: CaptureState.ready,
      contentHash: hash,
      byteSize: size,
    );

Memo _memo({String id = 'memo-1', String hash = 'abc123', int size = 10}) => Memo(
      id: id,
      state: 'captured',
      retention: MemoRetentionEnum.days30,
      contentHash: hash,
      byteSize: size,
      capturedAt: DateTime.now(),
      audioPruned: false,
      retentionStatus: 'active',
      prunesAt: null,
      audioPrunedAt: null,
      durationMs: null,
      codec: null,
      sampleRateHz: null,
    );

UploadState _state({
  required UploadStateStatusEnum status,
  Memo? memo,
  int offset = 10,
  int byteSize = 10,
  bool duplicate = false,
}) =>
    UploadState(
      status: status,
      byteSize: byteSize,
      offset: offset,
      memo: memo,
      duplicate: duplicate,
    );

void main() {
  group('verifyAck: the only door to acknowledged', () {
    test('a complete response whose memo matches the record verifies', () {
      final ack = verifyAck(
        _state(status: UploadStateStatusEnum.complete, memo: _memo()),
        _record(),
      );
      expect(ack, isNotNull);
      expect(ack!.memoId, 'memo-1');
      expect(ack.byteSize, 10);
      expect(ack.contentHash, 'abc123');
    });

    test('duplicate:true still verifies -- a replay is a success, not a conflict', () {
      final ack = verifyAck(
        _state(status: UploadStateStatusEnum.complete, memo: _memo(), duplicate: true),
        _record(),
      );
      expect(ack, isNotNull);
    });

    test('incomplete never verifies, whatever else the response carries', () {
      expect(
        verifyAck(
          _state(status: UploadStateStatusEnum.incomplete, memo: _memo()),
          _record(),
        ),
        isNull,
      );
    });

    test('complete with no memo attached does not verify', () {
      expect(
        verifyAck(_state(status: UploadStateStatusEnum.complete, memo: null), _record()),
        isNull,
      );
    });

    test('complete with an empty memo id does not verify', () {
      expect(
        verifyAck(
          _state(status: UploadStateStatusEnum.complete, memo: _memo(id: '')),
          _record(),
        ),
        isNull,
      );
    });

    test('a hash that disagrees with the record does not verify', () {
      expect(
        verifyAck(
          _state(status: UploadStateStatusEnum.complete, memo: _memo(hash: 'different')),
          _record(),
        ),
        isNull,
      );
    });

    test('a byte size that disagrees with the record does not verify', () {
      expect(
        verifyAck(
          _state(status: UploadStateStatusEnum.complete, memo: _memo(size: 999)),
          _record(),
        ),
        isNull,
      );
    });

    test('a record with no known hash or size can never verify -- never the memo\'s fault', () {
      expect(
        verifyAck(
          _state(status: UploadStateStatusEnum.complete, memo: _memo()),
          _record(hash: null, size: null),
        ),
        isNull,
      );
    });
  });
}
