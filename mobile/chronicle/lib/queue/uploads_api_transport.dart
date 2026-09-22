/// The real [UploadTransport]: a thin wrapper over the generated
/// `UploadsApi`. Every fact this depends on was verified directly against
/// the generated source during planning, and the fake server
/// (`test/queue/support/fake_chronicle_server.dart`) is built to the same
/// contract, so `engine.dart`'s whole test suite already exercises this
/// wrapper's SHAPE without a live server -- what remains is the plan's own
/// criterion 13, a manual pass against a real `chronicle serve`.
///
/// **The one thing worth restating here:** a chunk is sent through
/// `api_client.dart`'s special case for a lone [MultipartFile] whose
/// content type is not `multipart/form-data` -- raw bytes on the wire, one
/// stream, with a real `Content-Length` (`body.length`) rather than a
/// chunked transfer. `appendChunkWithHttpInfo` hardcodes the actual
/// `Content-Type` header to `application/octet-stream` itself, so the
/// [MultipartFile]'s own `contentType` is never read on this path and is
/// left at the constructor's default.
library;

import 'package:chronicle_api/api.dart';
import 'package:http/http.dart' show MultipartFile;

import 'upload_transport.dart';

class UploadsApiTransport implements UploadTransport {
  UploadsApiTransport(this._api);

  final UploadsApi _api;

  @override
  Future<UploadState> openUpload({
    required String idempotencyKey,
    required String contentHash,
    required int byteSize,
    String? retention,
  }) async {
    final state = await _api.openUpload(
      OpenUploadRequest(
        idempotencyKey: idempotencyKey,
        contentHash: contentHash,
        byteSize: byteSize,
        retention: _retentionEnum(retention),
      ),
    );
    return _require(state, 'openUpload');
  }

  @override
  Future<UploadState> appendChunk({
    required String uploadId,
    required int offset,
    required List<int> bytes,
  }) async {
    final state = await _api.appendChunk(
      uploadId,
      offset,
      MultipartFile.fromBytes('file', bytes),
    );
    return _require(state, 'appendChunk');
  }
}

/// `openUpload`/`appendChunk` are typed `Future<UploadState?>` only because
/// the generator treats every operation's "204, no body" shape uniformly --
/// this operation has no 204 in `openapi.yaml`; every real answer that does
/// not throw carries a body. A null here would mean the contract changed
/// out from under this client, not a case to route through
/// `classifyError`/`classifyResponse` as an ordinary outcome.
UploadState _require(UploadState? state, String call) {
  if (state == null) {
    throw StateError('$call answered with no body -- openapi.yaml declares one on every '
        'non-error response; the generated client and the server have drifted apart.');
  }
  return state;
}

OpenUploadRequestRetentionEnum? _retentionEnum(String? retention) {
  switch (retention) {
    case 'discard_now':
      return OpenUploadRequestRetentionEnum.discardNow;
    case 'days_30':
      return OpenUploadRequestRetentionEnum.days30;
    case 'forever':
      return OpenUploadRequestRetentionEnum.forever;
    default:
      // Anything else, including null, is "no opinion" -- meta.json's own
      // convention (`capture_record.dart`'s CaptureRecord.retention doc).
      // Never silently mapped to a specific enum value.
      return null;
  }
}
