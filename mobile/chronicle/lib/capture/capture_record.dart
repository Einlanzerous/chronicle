/// The capture record on disk: the durable interface CHRN-61 and CHRN-62 read.
///
/// ```
/// <filesDir>/captures/<capture_id>/
///     audio.opus       the stream, written continuously by CaptureService
///     meta.json        written and fsync'd BEFORE recording starts
///     lease            heartbeat + owner instance, rewritten by the service
///     audio.trimmed    only on a salvage; audio.opus is never edited
/// ```
///
/// ## Two things here are load-bearing and neither is obvious
///
/// **The idempotency key is minted before the first audio byte.** CHRN-20 asks
/// only that it be *"persisted by the client BEFORE the request goes out"*;
/// persisting it before the recording starts is stricter and free, and it means
/// a capture that survives a crash carries the same key it would have had — so
/// a retry after any interruption is a replay rather than a second memo. It is
/// derived from the capture id, which is right here even though the estate's
/// rule for *Switchyard* keys is never to derive one from an entity: CHRN-18 is
/// explicit that this key names an **arrival attempt** of one capture.
///
/// **`retention` starts null and stays null.** CHRN-62 fills it. It is not
/// defaulted to `days_30` here, because `store.Arrival`'s ratchet only ever
/// raises retention and there is no operation anywhere in the API that lowers
/// it — so a capture declared with a default can never afterwards be marked
/// `DISCARD NOW`. The queue holds the declaration until the choice exists or a
/// grace expires; that is CHRN-61's and CHRN-62's half of the same decision.
library;

import 'dart:convert';
import 'dart:io';
import 'dart:math';

/// Where a capture is in its life.
enum CaptureState {
  /// A recorder is writing, or was writing when the process died. **Never read
  /// this alone** — see `CaptureOwner`. On its own it cannot tell a crash from
  /// a swipe-away, and acting on it alone would trim a live file.
  recording,

  /// Structurally complete: every page present and CRC-valid, ending on a page
  /// boundary. `audio.opus` is what gets sent.
  ///
  /// **This does NOT mean the recorder stopped cleanly.** A capture recovered
  /// after a crash lands here too, whenever its stream turned out to be whole —
  /// which, measured on device, is the ordinary outcome of a kill, because the
  /// media server finalises the file when the client dies. What tells the two
  /// apart is [CaptureRecord.recoveredAt], not this state.
  ///
  /// It is also not a claim that nothing was lost. A recording cut at an fsync
  /// boundary by a flat battery is page-aligned and CRC-clean and lands here as
  /// well; nothing in the container distinguishes it, because Android's Ogg
  /// writer never sets the EOS flag on any file it produces, finished or not.
  ready,

  /// A trim actually shortened this. `audio.trimmed` is what gets sent, and
  /// `audio.opus` keeps every byte that ever reached the disk.
  ///
  /// **Narrower than it used to be**, and deliberately: this used to mean
  /// "recovered from an interruption", which put the chip on the commonest case
  /// and spent a byte-for-byte duplicate to say it. A signal that fires when
  /// nothing is wrong is not read when something is. CHRN-114.
  salvaged,

  /// Recovered, but no audio page ever reached the file.
  ///
  /// The memo existed — somebody spoke — and the bytes died inside the encoder
  /// before it flushed. This is kept, and is the one state that is visible
  /// without being sendable: deleting it would make the single worst loss this
  /// system can suffer completely silent.
  empty;

  static CaptureState parse(String? s) => CaptureState.values.firstWhere(
        (v) => v.name == s,
        orElse: () => CaptureState.recording,
      );
}

/// `meta.json`.
class CaptureRecord {
  const CaptureRecord({
    required this.captureId,
    required this.idempotencyKey,
    required this.startedAt,
    required this.state,
    this.retention,
    this.contentHash,
    this.byteSize,
    this.durationMs,
    this.trimOffset,
    this.recoveredAt,
  });

  final String captureId;
  final String idempotencyKey;

  /// When recording began, in local time with its offset.
  ///
  /// **The server has nowhere to put this yet**, and that is recorded rather
  /// than forgotten: `tier2.memos.captured_at` defaults to `now()` at finalise
  /// and `store.Arrival` carries no capture time, so a memo recorded in
  /// airplane mode on Monday and uploaded on Friday is stamped Friday. Writing
  /// it here from day one is what keeps that fixable — a client that never
  /// recorded the capture time makes the fix impossible rather than pending.
  final DateTime startedAt;

  final CaptureState state;

  /// Null until CHRN-62's confirm. Null means "no opinion", never `days_30`.
  final String? retention;

  final String? contentHash;
  final int? byteSize;
  final int? durationMs;

  /// Where the last CRC-valid page ended, on a salvage. `audio.opus` still
  /// holds every byte past it.
  final int? trimOffset;

  /// When recovery finished this capture, if recovery did.
  ///
  /// Null for a capture the app watched stop. Set for one it found already on
  /// disk — a crash, a force-stop, a process the system reclaimed.
  ///
  /// **This field is the only durable record that any of that happened.** A
  /// recovered whole capture is otherwise field-for-field identical to a clean
  /// stop: same state, same hash, same duration, one file. The Kotlin side logs
  /// the recorder's life to logcat, which is a ring buffer and is gone long
  /// before anybody asks. So this is not a nicety — without it the fact is not
  /// hidden, it is destroyed.
  ///
  /// It records **provenance, not completeness**: see [CaptureState.ready] for
  /// why "structurally complete" is the strongest claim available.
  final DateTime? recoveredAt;

  CaptureRecord copyWith({
    CaptureState? state,
    String? retention,
    String? contentHash,
    int? byteSize,
    int? durationMs,
    int? trimOffset,
    DateTime? recoveredAt,
  }) =>
      CaptureRecord(
        captureId: captureId,
        idempotencyKey: idempotencyKey,
        startedAt: startedAt,
        state: state ?? this.state,
        retention: retention ?? this.retention,
        contentHash: contentHash ?? this.contentHash,
        byteSize: byteSize ?? this.byteSize,
        durationMs: durationMs ?? this.durationMs,
        trimOffset: trimOffset ?? this.trimOffset,
        recoveredAt: recoveredAt ?? this.recoveredAt,
      );

  Map<String, Object?> toJson() => {
        'capture_id': captureId,
        'idempotency_key': idempotencyKey,
        'started_at': startedAt.toIso8601String(),
        'state': state.name,
        'retention': retention,
        'content_hash': contentHash,
        'byte_size': byteSize,
        'duration_ms': durationMs,
        'trim_offset': trimOffset,
        'recovered_at': recoveredAt?.toIso8601String(),
      };

  static CaptureRecord fromJson(Map<String, Object?> json) => CaptureRecord(
        captureId: json['capture_id']! as String,
        idempotencyKey: json['idempotency_key']! as String,
        startedAt: DateTime.parse(json['started_at']! as String),
        state: CaptureState.parse(json['state'] as String?),
        retention: json['retention'] as String?,
        contentHash: json['content_hash'] as String?,
        byteSize: json['byte_size'] as int?,
        durationMs: json['duration_ms'] as int?,
        trimOffset: json['trim_offset'] as int?,
        // Absent in records written before CHRN-114, which is exactly what a
        // capture the app watched stop should read as.
        recoveredAt: json['recovered_at'] == null
            ? null
            : DateTime.tryParse(json['recovered_at']! as String),
      );
}

/// The lease the recorder rewrites on a heartbeat.
///
/// In this process the service's own registry answers "is anybody recording
/// this?" directly and this file is not consulted. It matters to anything that
/// cannot ask in-process, and it is why the identity below is a **per-start
/// instance id** rather than a device boot id: a boot id tells a reboot from a
/// live device and says nothing about a killed recorder versus a running one
/// inside the same boot, which is the only question a lease has to answer.
class CaptureLease {
  const CaptureLease({
    required this.instanceId,
    required this.heartbeatAt,
    required this.heartbeatMs,
  });

  final String instanceId;
  final DateTime heartbeatAt;
  final int heartbeatMs;

  /// Three missed heartbeats. Only consulted when the owner cannot be asked.
  bool isStale(DateTime now) =>
      now.difference(heartbeatAt) > Duration(milliseconds: heartbeatMs * 3);

  static CaptureLease? tryParse(String body) {
    try {
      final json = jsonDecode(body) as Map<String, Object?>;
      return CaptureLease(
        instanceId: json['instance_id']! as String,
        heartbeatAt:
            DateTime.fromMillisecondsSinceEpoch(json['heartbeat_at']! as int),
        heartbeatMs: (json['heartbeat_ms'] as int?) ?? 5000,
      );
    } catch (_) {
      // A lease we cannot read is a lease that tells us nothing, which is the
      // same as not having one. It is never a reason to refuse a salvage.
      return null;
    }
  }
}

/// One capture's directory, and the only place that knows its file names.
class CaptureDir {
  CaptureDir(this.root, this.captureId);

  /// `<filesDir>/captures`, supplied by the platform side so Dart and Kotlin
  /// cannot disagree about where captures live.
  final Directory root;
  final String captureId;

  Directory get dir => Directory('${root.path}/$captureId');
  File get meta => File('${dir.path}/meta.json');
  File get lease => File('${dir.path}/lease');
  File get audio => File('${dir.path}/audio.opus');
  File get trimmed => File('${dir.path}/audio.trimmed');

  /// The file the queue sends: the trimmed stream on a salvage, else the
  /// original.
  File sendable(CaptureState state) =>
      state == CaptureState.salvaged ? trimmed : audio;

  Future<CaptureRecord?> readMeta() async {
    if (!await meta.exists()) return null;
    try {
      return CaptureRecord.fromJson(
        jsonDecode(await meta.readAsString()) as Map<String, Object?>,
      );
    } catch (_) {
      return null;
    }
  }

  Future<CaptureLease?> readLease() async {
    if (!await lease.exists()) return null;
    return CaptureLease.tryParse(await lease.readAsString());
  }

  /// Writes `meta.json` atomically: temp, flush, rename.
  ///
  /// A half-written record is worse than none — it would read as a capture with
  /// no idempotency key, and the next delivery would mint a second memo for the
  /// same audio.
  Future<void> writeMeta(CaptureRecord record) async {
    await dir.create(recursive: true);
    // Unique per write, not a fixed `meta.json.tmp`. Two writers sharing one
    // temp path is a race with a very bad worst case: one renames the file the
    // other is still filling, `readMeta` then fails to decode, and `recoverAll`
    // skips that capture forever while `refresh` never lists it — the audio is
    // on disk and the app cannot reach it. Cheap insurance against ever having
    // two passes again.
    final tmp = File('${meta.path}.${_tempSeq++}-'
        '${DateTime.now().microsecondsSinceEpoch}.tmp');
    await tmp.writeAsString(
      const JsonEncoder.withIndent('  ').convert(record.toJson()),
      flush: true,
    );
    await tmp.rename(meta.path);
  }
}

int _tempSeq = 0;

/// A capture id and the key derived from it, minted together and never apart.
class CaptureIdentity {
  CaptureIdentity._(this.captureId, this.idempotencyKey);

  final String captureId;
  final String idempotencyKey;

  /// `chr-cap-<uuid>` is 44 characters, comfortably over `OpenUploadRequest`'s
  /// 16-character floor.
  factory CaptureIdentity.mint() {
    final id = _uuidV4();
    return CaptureIdentity._(id, 'chr-cap-$id');
  }
}

final _random = Random.secure();

/// A v4 UUID, without taking a dependency for sixteen bytes of randomness.
String _uuidV4() {
  final b = List<int>.generate(16, (_) => _random.nextInt(256));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  String hex(int from, int to) =>
      b.sublist(from, to).map((x) => x.toRadixString(16).padLeft(2, '0')).join();
  return '${hex(0, 4)}-${hex(4, 6)}-${hex(6, 8)}-${hex(8, 10)}-${hex(10, 16)}';
}
