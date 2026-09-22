/// The queue's own state for one capture: `upload.json`, written and owned
/// only by the queue engine.
///
/// ```
/// <filesDir>/captures/<capture_id>/
///     upload.json      the queue's record; never touches audio, meta.json,
///                       or the lease -- see CaptureDir for those.
/// ```
///
/// **The rule this file exists to enforce: persist facts about the capture;
/// derive facts about the device.** `signedOut` and `wrongHost` are
/// properties of the device's session and configured host, not of any one
/// capture, and are held in memory by the engine (`device_block.dart`)
/// rather than written here. What lives in a [QueueRecord] is only ever true
/// of THIS capture: whether its bytes still match what was declared, how
/// many times sending it has been tried, and what the server has said about
/// it.
///
/// **There is no persisted "uploading".** CHRN-60 learned this the hard way:
/// `state: recording` on disk was the wrong oracle for "is this still being
/// written", because a crash and a clean stop leave the same flag on disk.
/// The same mistake here would be a `sending` flag a killed engine leaves
/// behind, read by the next engine as still true. Sending is a live fact of
/// a running engine and is never durable.
library;

import 'dart:convert';
import 'dart:io';

import '../capture/capture_record.dart';

/// Where a capture is in the QUEUE's state machine. Persisted.
enum QueueStatus {
  /// Not yet acknowledged. May be waiting on retention, on backoff, on a
  /// device block, or genuinely about to be sent -- none of that is stored
  /// here; see `device_block.dart`, `backoff.dart` and `retention_gate.dart`
  /// for what layers on top of a `pending` record.
  pending,

  /// The server has confirmed receipt of exactly these bytes. Terminal, and
  /// reachable only through `verifyAck` (`ack.dart`) -- never set directly.
  acknowledged,

  /// The sendable file's length no longer matches what `meta.json` declared.
  /// Re-checked on every pass and clears itself the moment the lengths agree
  /// again, so it is a symptom, not a permanent verdict.
  blockedLocalFileChanged,

  /// Parked. The engine will not retry this capture on its own; a person
  /// invoking "Try again" is what moves it back to `pending`.
  rejected,
}

/// Why a capture is `rejected`. Distinct reasons because they read
/// differently on the queue screen, and because two of them (`keyReused`,
/// `hashMismatch`) are questions about content that no retry can fix, not
/// about the network.
enum RejectReason {
  /// 409 `idempotency_key_reused`: this key has already produced a
  /// different memo. Parked at once -- minting a fresh key is CHRN-61's own
  /// decision to make deliberately, never something the engine does
  /// silently.
  keyReused,

  /// 422 `content_hash_mismatch`: the server destroyed the session because
  /// what arrived did not hash to what was declared.
  hashMismatch,

  /// A resume that made no progress, or `oversend`, three times running.
  /// Something about the protocol exchange itself is wrong, not about this
  /// one attempt.
  protocol,

  /// Any other real 4xx the server sent, three times running.
  serverRefused,
}

/// The class the most recent attempt's outcome fell into. Drives
/// `failureStreak` and whether a capture is retried or parked; not a
/// judgement on its own -- see `failure.dart`.
enum FailureClass {
  /// No HTTP answer reached the client at all: a socket, TLS, timeout or
  /// client error, in either the shape the generated client wraps or the
  /// raw shape it sometimes leaks. Ends the whole pass: nothing else will
  /// work either.
  network,

  /// 5xx or 503 on THIS capture, while other captures are still
  /// succeeding. Never escalates to `rejected` on its own.
  transientServer,

  /// A resume (an `incomplete` answer, a 408, or an offset-409) whose
  /// stated offset did not advance past where the chunk was sent from.
  /// Left unguarded this loops forever against a server that keeps losing
  /// staging.
  noProgress,

  /// 422 `oversend`, repeated.
  protocolOversend,

  /// 409 `idempotency_key_reused`. Also carried as [RejectReason.keyReused]
  /// once the capture is actually parked.
  rejectedKeyReused,

  /// 422 `content_hash_mismatch`. Also carried as
  /// [RejectReason.hashMismatch] once the capture is actually parked.
  rejectedHashMismatch,

  /// Any other real 4xx the server sent.
  rejectedOther,
}

/// One capture's queue record: `upload.json`.
class QueueRecord {
  const QueueRecord({
    required this.status,
    required this.enqueuedAt,
    this.attemptCount = 0,
    this.lastAttemptAt,
    this.lastFailureClass,
    this.lastFailureCode,
    this.failureStreak = 0,
    this.rejectReason,
    this.memoId,
    this.acknowledgedAt,
  });

  final QueueStatus status;

  /// The moment the queue first saw this capture sendable. The retention
  /// grace (`retention_gate.dart`) runs from here, never from `startedAt`
  /// -- a kill between "ready" and "enqueue" can therefore only extend the
  /// choosing window, never shorten it.
  final DateTime enqueuedAt;

  final int attemptCount;
  final DateTime? lastAttemptAt;

  /// The class of the MOST RECENT outcome, shown on the queue screen as the
  /// last-attempt line. Distinct from [failureStreak], which counts how
  /// many attempts in a row have shared this class.
  final FailureClass? lastFailureClass;

  /// A short machine code for the last outcome (an HTTP status, a
  /// Chronicle error `code`), for the screen and for debugging. Never
  /// parsed back into behaviour -- [lastFailureClass] is what the engine
  /// acts on.
  final String? lastFailureCode;

  /// How many CONSECUTIVE attempts have ended in [lastFailureClass]. Reset
  /// by an attempt that ends differently, or by this capture's own
  /// acknowledgement. What every "after N consecutive" rule counts.
  final int failureStreak;

  /// Set only when `status == rejected`.
  final RejectReason? rejectReason;

  /// Set only when `status == acknowledged`, by `verifyAck` and nothing
  /// else.
  final String? memoId;
  final DateTime? acknowledgedAt;

  bool get isTerminal => status == QueueStatus.acknowledged;

  QueueRecord copyWith({
    QueueStatus? status,
    int? attemptCount,
    DateTime? lastAttemptAt,
    FailureClass? lastFailureClass,
    bool clearLastFailureClass = false,
    String? lastFailureCode,
    bool clearLastFailureCode = false,
    int? failureStreak,
    RejectReason? rejectReason,
    bool clearRejectReason = false,
    String? memoId,
    DateTime? acknowledgedAt,
  }) =>
      QueueRecord(
        status: status ?? this.status,
        enqueuedAt: enqueuedAt,
        attemptCount: attemptCount ?? this.attemptCount,
        lastAttemptAt: lastAttemptAt ?? this.lastAttemptAt,
        lastFailureClass: clearLastFailureClass
            ? null
            : (lastFailureClass ?? this.lastFailureClass),
        lastFailureCode: clearLastFailureCode
            ? null
            : (lastFailureCode ?? this.lastFailureCode),
        failureStreak: failureStreak ?? this.failureStreak,
        rejectReason:
            clearRejectReason ? null : (rejectReason ?? this.rejectReason),
        memoId: memoId ?? this.memoId,
        acknowledgedAt: acknowledgedAt ?? this.acknowledgedAt,
      );

  Map<String, Object?> toJson() => {
        'status': status.name,
        'enqueued_at': enqueuedAt.toIso8601String(),
        'attempt_count': attemptCount,
        'last_attempt_at': lastAttemptAt?.toIso8601String(),
        'last_failure_class': lastFailureClass?.name,
        'last_failure_code': lastFailureCode,
        'failure_streak': failureStreak,
        'reject_reason': rejectReason?.name,
        'memo_id': memoId,
        'acknowledged_at': acknowledgedAt?.toIso8601String(),
      };

  static QueueRecord fromJson(Map<String, Object?> json) => QueueRecord(
        status: _enumOrDefault(
            QueueStatus.values, json['status'] as String?, QueueStatus.pending),
        enqueuedAt: DateTime.parse(json['enqueued_at']! as String),
        attemptCount: (json['attempt_count'] as int?) ?? 0,
        lastAttemptAt: json['last_attempt_at'] == null
            ? null
            : DateTime.tryParse(json['last_attempt_at']! as String),
        lastFailureClass: _enumOrNull(
            FailureClass.values, json['last_failure_class'] as String?),
        lastFailureCode: json['last_failure_code'] as String?,
        failureStreak: (json['failure_streak'] as int?) ?? 0,
        rejectReason: _enumOrNull(
            RejectReason.values, json['reject_reason'] as String?),
        memoId: json['memo_id'] as String?,
        acknowledgedAt: json['acknowledged_at'] == null
            ? null
            : DateTime.tryParse(json['acknowledged_at']! as String),
      );

  /// A freshly enqueued capture: `pending`, nothing tried yet.
  factory QueueRecord.fresh(DateTime enqueuedAt) =>
      QueueRecord(status: QueueStatus.pending, enqueuedAt: enqueuedAt);
}

T _enumOrDefault<T extends Enum>(List<T> values, String? name, T fallback) {
  if (name == null) return fallback;
  for (final v in values) {
    if (v.name == name) return v;
  }
  return fallback;
}

T? _enumOrNull<T extends Enum>(List<T> values, String? name) {
  if (name == null) return null;
  for (final v in values) {
    if (v.name == name) return v;
  }
  return null;
}

/// One capture's queue file: `upload.json`, sibling to [CaptureDir]'s files.
///
/// **This is the whole of the engine's write surface.** It can read the
/// sendable audio and `meta.json` (through the [CaptureDir] it wraps) but
/// never write or delete them -- CHRN-20 section 4's promise ("the phone
/// still holds the file") and CHRN-60's rule that nothing shrinks a capture
/// the server has not acknowledged therefore hold by construction here, not
/// by discipline.
class QueueDir {
  QueueDir(this.capture);

  final CaptureDir capture;

  File get uploadFile => File('${capture.dir.path}/upload.json');

  Future<QueueRecord?> read() async {
    if (!await uploadFile.exists()) return null;
    try {
      return QueueRecord.fromJson(
        jsonDecode(await uploadFile.readAsString()) as Map<String, Object?>,
      );
    } catch (_) {
      // Absent or corrupt reads the same: the capture is re-queued from
      // scratch, and the server re-tells the ack on the next open. Losing
      // this file costs one round trip, never the audio.
      return null;
    }
  }

  /// Atomic: temp, flush, rename -- the same pattern as
  /// `CaptureDir.writeMeta`, for the same reason. A half-written
  /// `upload.json` would read as "never queued" and re-enqueue a capture
  /// that may already be `acknowledged`, which costs a needless re-open
  /// (answered `duplicate: true`) rather than a lost memo -- but atomic
  /// writes cost nothing and make even that impossible.
  Future<void> write(QueueRecord record) async {
    await capture.dir.create(recursive: true);
    final tmp = File('${uploadFile.path}.${_tempSeq++}-'
        '${DateTime.now().microsecondsSinceEpoch}.tmp');
    await tmp.writeAsString(
      const JsonEncoder.withIndent('  ').convert(record.toJson()),
      flush: true,
    );
    await tmp.rename(uploadFile.path);
  }
}

int _tempSeq = 0;
