/// The engine's transport seam: exactly the two calls one attempt makes,
/// shaped after the generated `UploadsApi`
/// (`packages/chronicle_api/lib/api/uploads_api.dart`) so the real
/// implementation (build step 3) is a thin wrapper over
/// `UploadsApi.openUpload`/`appendChunk`, and a fake can stand in for it
/// exactly.
///
/// **Both methods must be able to throw all three shapes a real attempt can
/// produce** -- a real server answer (`ApiException(code, message)`, no
/// `innerException`), a network failure the generated client wraps
/// (`ApiException.withInner`), or one that escapes the wrap and reaches the
/// caller raw (see `failure.dart`'s library doc, and `api_client.dart`'s own
/// `return Response.fromStream(response)` with no `await`). `classifyError`
/// is the one place that ambiguity is resolved; this interface exists only
/// so a fake can produce every one of those shapes on demand, at any call.
///
/// Deliberately **not** here: `GET /memos/uploads/{id}` and `DELETE`. The
/// plan resolves "did my last chunk land" by re-opening (`openUpload` again,
/// same key), never by polling -- `GET` is a 404 once an upload completes
/// (`ClearUploadKey`), so it cannot even answer the question a resume needs.
/// `DELETE` (abandon) has no caller yet: a rejected capture is parked, not
/// abandoned.
library;

import 'package:chronicle_api/api.dart';

abstract class UploadTransport {
  /// `POST /memos/uploads`. [retention] is `meta.json`'s value, passed
  /// through unchanged -- null means "no opinion", never a default.
  Future<UploadState> openUpload({
    required String idempotencyKey,
    required String contentHash,
    required int byteSize,
    String? retention,
  });

  /// `PATCH /memos/uploads/{id}`, one chunk, raw bytes starting at [offset].
  Future<UploadState> appendChunk({
    required String uploadId,
    required int offset,
    required List<int> bytes,
  });
}
