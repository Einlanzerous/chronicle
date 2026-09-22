/// The screen label for one queue row: a pure function from persisted state
/// (plus the one live fact, `sending`) to the exact words CHRN-61's plan
/// names -- `QUEUED` · `SENDING` · `AWAITING RETENTION` · `SIGN IN TO SEND`
/// · `NOT SENT — <reason>` · `SENT`. Kept separate from the widget so the
/// mapping is tested directly, the same way `failure.dart`'s classification
/// is: a screen is the last place a wrong label should be caught.
///
/// **`SENT` is reachable only when [record.status] is `acknowledged`** --
/// criterion 10. Every other branch is checked first specifically so
/// nothing can shadow that one.
library;

import 'device_block.dart';
import 'queue_record.dart';
import 'retention_gate.dart';

/// [captureId]'s row label. [allRecords] is every OTHER capture's record
/// currently known, keyed by id -- needed only for the `NOT SENT — SERVER
/// ERROR` evidence rule below. [retention] and [enqueuedAt] are the
/// capture's own declared retention and its record's enqueue time, for the
/// `AWAITING RETENTION` gate.
String queueLabel({
  required String captureId,
  required QueueRecord? record,
  required Map<String, QueueRecord> allRecords,
  required DeviceBlock? deviceBlock,
  required String? sendingCaptureId,
  required String? retention,
  required DateTime enqueuedAt,
  required DateTime now,
}) {
  if (sendingCaptureId == captureId) return 'SENDING';
  if (record == null) return 'QUEUED'; // not yet scanned by a pass

  if (record.status == QueueStatus.acknowledged) return 'SENT';

  if (record.status == QueueStatus.rejected) {
    return 'NOT SENT — ${_rejectReasonWords(record.rejectReason)}';
  }
  if (record.status == QueueStatus.blockedLocalFileChanged) {
    return 'NOT SENT — FILE CHANGED';
  }

  // Only `pending` is left. A device block explains "why hasn't this gone
  // anywhere" before anything about THIS capture's own history does.
  if (deviceBlock != null) {
    return deviceBlock.reason == DeviceBlockReason.signedOut
        ? 'SIGN IN TO SEND'
        : 'NOT SENT — WRONG HOST';
  }

  if (!retentionGateOpen(retention: retention, enqueuedAt: enqueuedAt, now: now)) {
    return 'AWAITING RETENTION';
  }

  // A 5xx only earns this capture its own label once there is EVIDENCE the
  // server is otherwise answering -- some other capture acknowledged more
  // recently than this one's last attempt. Without that, a full outage
  // would individually blame every capture in the queue for the same one
  // fact. See the plan's own "loose ends" / failure table note.
  if (record.lastFailureClass == FailureClass.transientServer &&
      _otherCaptureAckedSince(allRecords, captureId, record.lastAttemptAt)) {
    return 'NOT SENT — SERVER ERROR';
  }

  return 'QUEUED';
}

String _rejectReasonWords(RejectReason? reason) => switch (reason) {
      RejectReason.keyReused => 'KEY CONFLICT',
      RejectReason.hashMismatch => 'HASH MISMATCH',
      RejectReason.protocol => 'PROTOCOL ERROR',
      RejectReason.serverRefused => 'REFUSED',
      null => 'REFUSED', // unreached in practice -- rejected always sets one
    };

bool _otherCaptureAckedSince(
  Map<String, QueueRecord> allRecords,
  String captureId,
  DateTime? since,
) {
  if (since == null) return false;
  for (final entry in allRecords.entries) {
    if (entry.key == captureId) continue;
    final ackedAt = entry.value.acknowledgedAt;
    if (ackedAt != null && ackedAt.isAfter(since)) return true;
  }
  return false;
}
