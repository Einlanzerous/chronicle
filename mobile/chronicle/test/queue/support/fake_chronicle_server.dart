/// A faithful in-Dart model of the upload protocol `openapi.yaml` and
/// `internal/upload/upload.go` describe -- offsets, the two 409 shapes, 422
/// `oversend` and `content_hash_mismatch`, 429 `too_many_open_uploads`, key
/// replay, and completion. Faithful to the DOCUMENTED contract (the spec,
/// plus the specific Go facts CHRN-61's plan verified); one behaviour is a
/// deliberate, flagged simplification rather than a verified fact -- see
/// [FakeChronicleServer.append]'s comment on a completed session's `PATCH`.
/// Step 3's real-server harness is what confirms or corrects that against
/// the genuine article.
///
/// Paired with [FakeUploadTransport], which implements `UploadTransport`
/// over this server and can additionally inject a network-layer fault --
/// wrapped or raw, before the server sees a call or after it has already
/// applied one -- so a test can produce every shape `classifyError` has to
/// handle without a real socket.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:chronicle/queue/upload_transport.dart';
import 'package:chronicle_api/api.dart';
import 'package:crypto/crypto.dart';

class _Session {
  _Session({
    required this.uploadId,
    required this.idempotencyKey,
    required this.contentHash,
    required this.byteSize,
    required this.retention,
    required this.recordedAt,
  });

  final String uploadId;
  final String idempotencyKey;
  final String contentHash;
  final int byteSize;
  final String? retention;
  final DateTime recordedAt;
  final BytesBuilder received = BytesBuilder(copy: true);
  Memo? memo;

  int get offset => received.length;
  bool get complete => memo != null;
}

class FakeChronicleServer {
  final Map<String, _Session> _byKey = {};
  final Map<String, _Session> _byUploadId = {};
  int _nextId = 1;

  /// Every memo this server has ever finalised, in commit order -- what a
  /// test reads to assert "exactly one memo" or "ten distinct hashes".
  final List<Memo> memos = [];

  /// Settable by a test: the next [open] answers 429 `too_many_open_uploads`
  /// regardless of key, simulating the deployment's open-session cap.
  bool forcePendingLimit = false;

  UploadState open({
    required String idempotencyKey,
    required String contentHash,
    required int byteSize,
    required DateTime recordedAt,
    String? retention,
  }) {
    final existing = _byKey[idempotencyKey];
    if (existing != null) {
      if (existing.contentHash != contentHash || existing.byteSize != byteSize) {
        // A reused key with a different declaration. No session to
        // describe -- the `Error` shape, not `UploadState`, and critically
        // NO `offset` field: that is what lets `classifyResponse` tell this
        // apart from a resume (`openapi.yaml`'s `UploadConflict`
        // `oneOf` -- see its own comment on why the two shapes are
        // disjoint by field, not by a shared envelope).
        throw ApiException(
          409,
          jsonEncode({
            'code': 'idempotency_key_reused',
            'message': 'this key already names a different upload',
          }),
        );
      }
      if (existing.complete) {
        return UploadState(
          status: UploadStateStatusEnum.complete,
          uploadId: existing.uploadId,
          byteSize: existing.byteSize,
          offset: existing.byteSize,
          memo: existing.memo,
          duplicate: true,
        );
      }
      return UploadState(
        status: UploadStateStatusEnum.incomplete,
        uploadId: existing.uploadId,
        byteSize: existing.byteSize,
        offset: existing.offset,
        duplicate: false,
      );
    }
    if (forcePendingLimit) {
      throw ApiException(
        429,
        jsonEncode({'code': 'too_many_open_uploads', 'message': 'too many open uploads'}),
      );
    }
    final session = _Session(
      uploadId: 'up-${_nextId++}',
      idempotencyKey: idempotencyKey,
      contentHash: contentHash,
      byteSize: byteSize,
      retention: retention,
      recordedAt: recordedAt,
    );
    _byKey[idempotencyKey] = session;
    _byUploadId[session.uploadId] = session;
    return UploadState(
      status: UploadStateStatusEnum.incomplete,
      uploadId: session.uploadId,
      byteSize: session.byteSize,
      offset: 0,
      duplicate: false,
    );
  }

  UploadState append({
    required String uploadId,
    required int offset,
    required List<int> bytes,
  }) {
    final session = _byUploadId[uploadId];
    if (session == null) {
      // Unknown to this server: never issued, or `ErrStagingLost` --
      // staging lost is documented as "the session is alive and this is an
      // instruction for continuing it", offset 0, `UploadState`-shaped, NOT
      // a 404. See `openapi.yaml`'s `UploadConflict`, "Resume" branch.
      throw ApiException(
        409,
        jsonEncode({
          'status': 'incomplete',
          'upload_id': uploadId,
          'byte_size': 0,
          'offset': 0,
          'duplicate': false,
        }),
      );
    }
    if (session.complete) {
      // Real `ClearUploadKey` deletes the session row once an upload
      // completes, and a verified fact is that `GET` then 404s (see
      // `FakeChronicleServer.getStatus`). What a completed id's `PATCH`
      // answers is NOT independently verified against the Go source this
      // session -- this fake chooses the "409 resync" reading the plan's
      // own words commit to for the concurrent-engine race ("a 409-offset
      // resync and an idempotent complete, never a second memo or an
      // unverified ack"), because it is the SAFE direction: it can never be
      // mistaken for a rejection. Step 3's real-server harness is where
      // this gets confirmed or corrected.
      throw ApiException(
        409,
        jsonEncode({
          'status': 'complete',
          'upload_id': session.uploadId,
          'byte_size': session.byteSize,
          'offset': session.byteSize,
          'duplicate': true,
        }),
      );
    }
    if (offset != session.offset) {
      throw ApiException(
        409,
        jsonEncode({
          'status': 'incomplete',
          'upload_id': session.uploadId,
          'byte_size': session.byteSize,
          'offset': session.offset,
          'duplicate': false,
        }),
      );
    }
    if (offset + bytes.length > session.byteSize) {
      throw ApiException(
        422,
        jsonEncode({'code': 'oversend', 'message': 'more bytes than declared'}),
      );
    }

    session.received.add(bytes is Uint8List ? bytes : Uint8List.fromList(bytes));

    if (session.offset < session.byteSize) {
      return UploadState(
        status: UploadStateStatusEnum.incomplete,
        uploadId: session.uploadId,
        byteSize: session.byteSize,
        offset: session.offset,
        duplicate: false,
      );
    }

    // Every declared byte is in. Commit is gated on the hash actually
    // matching -- `content_hash` is the memo's identity (CHRN-20), checked
    // on completion, exactly like the real server.
    final hash = sha256.convert(session.received.toBytes()).toString();
    if (hash != session.contentHash) {
      // The real server destroys the session on a mismatch. This fake
      // matches that: the key and the id both stop resolving, so a retry
      // (which this client never issues without minting a fresh key first)
      // would see "unknown session", not a stale, half-committed one.
      _byKey.remove(session.idempotencyKey);
      _byUploadId.remove(session.uploadId);
      throw ApiException(
        422,
        jsonEncode({
          'code': 'content_hash_mismatch',
          'message': 'bytes did not match the declared hash',
        }),
      );
    }

    final memo = Memo(
      id: 'memo-${_nextId++}',
      state: 'captured',
      retention: _memoRetention(session.retention),
      contentHash: session.contentHash,
      byteSize: session.byteSize,
      capturedAt: DateTime.now().toUtc(),
      recordedAt: session.recordedAt,
      audioPruned: false,
      retentionStatus: 'scheduled',
      prunesAt: null,
      audioPrunedAt: null,
      durationMs: 0,
      codec: 'opus',
      sampleRateHz: 48000,
    );
    session.memo = memo;
    memos.add(memo);
    return UploadState(
      status: UploadStateStatusEnum.complete,
      uploadId: session.uploadId,
      byteSize: session.byteSize,
      offset: session.byteSize,
      memo: memo,
      duplicate: false,
    );
  }

  /// `GET /memos/uploads/{id}`. Not called by the engine (the plan resolves
  /// ambiguity by re-opening, never by polling) -- kept here for
  /// completeness and so a test can assert the verified fact directly: a
  /// 404 (modelled as `null`; the real transport would throw) once
  /// complete.
  UploadState? getStatus(String uploadId) {
    final session = _byUploadId[uploadId];
    if (session == null || session.complete) return null;
    return UploadState(
      status: UploadStateStatusEnum.incomplete,
      uploadId: session.uploadId,
      byteSize: session.byteSize,
      offset: session.offset,
      duplicate: false,
    );
  }
}

MemoRetentionEnum _memoRetention(String? retention) {
  switch (retention) {
    case 'discard_now':
      return MemoRetentionEnum.discardNow;
    case 'forever':
      return MemoRetentionEnum.forever;
    default:
      return MemoRetentionEnum.days30;
  }
}

/// A network-layer fault a test injects into [FakeUploadTransport], on top
/// of whatever [FakeChronicleServer] would otherwise answer.
class TransportFault {
  /// Thrown before the server ever sees this call -- a connection that
  /// never reached it.
  const TransportFault.beforeSend(this.error) : afterSend = false;

  /// The server call happens for real (so its state updates -- a memo can
  /// commit), but [error] is thrown to the caller INSTEAD of the server's
  /// real answer. The specific ambiguity CHRN-61 exists for: a response
  /// lost after the server already committed.
  const TransportFault.afterSend(this.error) : afterSend = true;

  final Object error;
  final bool afterSend;
}

/// [UploadTransport] over a [FakeChronicleServer], with an injectable fault
/// hook for simulating the network conditions the server itself cannot
/// produce (a dropped socket, a stalled read).
class FakeUploadTransport implements UploadTransport {
  FakeUploadTransport(this.server);

  final FakeChronicleServer server;

  /// Called before every call this transport makes, numbered from 1 across
  /// BOTH [openUpload] and [appendChunk]. Return a [TransportFault] to
  /// disrupt this call, or null to let the server answer normally. May
  /// itself be `async` and `await` -- a concurrency test uses that to hold
  /// one engine's call open until another engine has made progress,
  /// producing a deterministic interleaving instead of trusting whatever
  /// order the microtask queue happens to pick.
  FutureOr<TransportFault?> Function(int callNumber, {required bool isOpen})? onBeforeCall;

  int calls = 0;

  @override
  Future<UploadState> openUpload({
    required String idempotencyKey,
    required String contentHash,
    required int byteSize,
    required DateTime recordedAt,
    String? retention,
  }) =>
      _call(
        isOpen: true,
        real: () => server.open(
          idempotencyKey: idempotencyKey,
          contentHash: contentHash,
          byteSize: byteSize,
          recordedAt: recordedAt,
          retention: retention,
        ),
      );

  @override
  Future<UploadState> appendChunk({
    required String uploadId,
    required int offset,
    required List<int> bytes,
  }) =>
      _call(
        isOpen: false,
        real: () => server.append(uploadId: uploadId, offset: offset, bytes: bytes),
      );

  Future<UploadState> _call({
    required bool isOpen,
    required UploadState Function() real,
  }) async {
    calls++;
    final fault = await onBeforeCall?.call(calls, isOpen: isOpen);
    if (fault != null && !fault.afterSend) {
      throw fault.error;
    }
    final result = real();
    if (fault != null && fault.afterSend) {
      throw fault.error;
    }
    return result;
  }
}
