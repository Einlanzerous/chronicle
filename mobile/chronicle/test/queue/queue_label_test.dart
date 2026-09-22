import 'package:chronicle/queue/device_block.dart';
import 'package:chronicle/queue/queue_label.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  final now = DateTime(2026, 1, 1, 12);

  String label({
    QueueRecord? record,
    Map<String, QueueRecord> allRecords = const {},
    DeviceBlock? deviceBlock,
    String? sendingCaptureId,
    String? retention,
    DateTime? enqueuedAt,
  }) =>
      queueLabel(
        captureId: 'a',
        record: record,
        allRecords: allRecords,
        deviceBlock: deviceBlock,
        sendingCaptureId: sendingCaptureId,
        retention: retention,
        enqueuedAt: enqueuedAt ?? now,
        now: now,
      );

  test('SENT is reachable only through acknowledged -- criterion 10, at the label layer', () {
    expect(
      label(record: QueueRecord(status: QueueStatus.acknowledged, enqueuedAt: now)),
      'SENT',
    );
    for (final status in QueueStatus.values.where((s) => s != QueueStatus.acknowledged)) {
      expect(
        label(record: QueueRecord(status: status, enqueuedAt: now)),
        isNot('SENT'),
        reason: '$status must never read as SENT',
      );
    }
  });

  test('a capture the queue has not scanned yet reads QUEUED, not blank', () {
    expect(label(record: null), 'QUEUED');
  });

  test('SENDING wins over everything else -- it is the live fact', () {
    expect(
      label(
        record: QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
        sendingCaptureId: 'a',
      ),
      'SENDING',
    );
  });

  group('rejected -- NOT SENT, with the specific reason', () {
    test('keyReused', () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.rejected,
            enqueuedAt: now,
            rejectReason: RejectReason.keyReused,
          ),
        ),
        'NOT SENT — KEY CONFLICT',
      );
    });

    test('hashMismatch', () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.rejected,
            enqueuedAt: now,
            rejectReason: RejectReason.hashMismatch,
          ),
        ),
        'NOT SENT — HASH MISMATCH',
      );
    });

    test('protocol', () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.rejected,
            enqueuedAt: now,
            rejectReason: RejectReason.protocol,
          ),
        ),
        'NOT SENT — PROTOCOL ERROR',
      );
    });

    test('serverRefused', () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.rejected,
            enqueuedAt: now,
            rejectReason: RejectReason.serverRefused,
          ),
        ),
        'NOT SENT — REFUSED',
      );
    });
  });

  test('a file that no longer matches its declaration reads NOT SENT — FILE CHANGED', () {
    expect(
      label(
        record: QueueRecord(status: QueueStatus.blockedLocalFileChanged, enqueuedAt: now),
      ),
      'NOT SENT — FILE CHANGED',
    );
  });

  group('a device block explains a pending capture before anything else does', () {
    test('signedOut reads SIGN IN TO SEND', () {
      expect(
        label(
          record: QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
          deviceBlock: const DeviceBlock(
            reason: DeviceBlockReason.signedOut,
            serverUrl: 'https://chronicle-direct.example.com',
            tokenDigest: '',
          ),
        ),
        'SIGN IN TO SEND',
      );
    });

    test('wrongHost reads NOT SENT — WRONG HOST', () {
      expect(
        label(
          record: QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
          deviceBlock: const DeviceBlock(
            reason: DeviceBlockReason.wrongHost,
            serverUrl: 'https://chronicle.example.com',
            tokenDigest: 'digest',
          ),
        ),
        'NOT SENT — WRONG HOST',
      );
    });

    test('a device block never shadows an already-rejected capture', () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.rejected,
            enqueuedAt: now,
            rejectReason: RejectReason.hashMismatch,
          ),
          deviceBlock: const DeviceBlock(
            reason: DeviceBlockReason.signedOut,
            serverUrl: 'https://chronicle-direct.example.com',
            tokenDigest: '',
          ),
        ),
        'NOT SENT — HASH MISMATCH',
      );
    });
  });

  test('the retention gate closed reads AWAITING RETENTION', () {
    expect(
      label(
        record: QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
        retention: null,
        enqueuedAt: now.add(const Duration(hours: 1)), // "now" is before enqueuedAt + grace
      ),
      'AWAITING RETENTION',
    );
  });

  test('a declared retention never reads AWAITING RETENTION, whatever the timing', () {
    expect(
      label(
        record: QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
        retention: 'days_30',
        enqueuedAt: now.add(const Duration(hours: 1)),
      ),
      'QUEUED',
    );
  });

  group('a transient server error only names itself with evidence', () {
    test('no other capture has acknowledged since -- stays QUEUED, not individually blamed', () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.pending,
            enqueuedAt: now,
            lastFailureClass: FailureClass.transientServer,
            lastAttemptAt: now,
          ),
          allRecords: {
            'a': QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
            'b': QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
          },
        ),
        'QUEUED',
      );
    });

    test('another capture acknowledged after this one\'s last attempt -- NOT SENT — SERVER ERROR',
        () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.pending,
            enqueuedAt: now,
            lastFailureClass: FailureClass.transientServer,
            lastAttemptAt: now,
          ),
          allRecords: {
            'a': QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
            'b': QueueRecord(
              status: QueueStatus.acknowledged,
              enqueuedAt: now,
              acknowledgedAt: now.add(const Duration(minutes: 1)),
            ),
          },
        ),
        'NOT SENT — SERVER ERROR',
      );
    });

    test('an ack BEFORE this capture\'s last attempt is not evidence of anything current', () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.pending,
            enqueuedAt: now,
            lastFailureClass: FailureClass.transientServer,
            lastAttemptAt: now,
          ),
          allRecords: {
            'a': QueueRecord(status: QueueStatus.pending, enqueuedAt: now),
            'b': QueueRecord(
              status: QueueStatus.acknowledged,
              enqueuedAt: now,
              acknowledgedAt: now.subtract(const Duration(minutes: 1)),
            ),
          },
        ),
        'QUEUED',
      );
    });

    test('a network failure never gets the server-error label, evidence or not', () {
      expect(
        label(
          record: QueueRecord(
            status: QueueStatus.pending,
            enqueuedAt: now,
            lastFailureClass: FailureClass.network,
            lastAttemptAt: now,
          ),
          allRecords: {
            'b': QueueRecord(
              status: QueueStatus.acknowledged,
              enqueuedAt: now,
              acknowledgedAt: now.add(const Duration(minutes: 1)),
            ),
          },
        ),
        'QUEUED',
      );
    });
  });

  test('a never-attempted pending capture with an open gate reads QUEUED', () {
    expect(
      label(record: QueueRecord(status: QueueStatus.pending, enqueuedAt: now)),
      'QUEUED',
    );
  });
}
