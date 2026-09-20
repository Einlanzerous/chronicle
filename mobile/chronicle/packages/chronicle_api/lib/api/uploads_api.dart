//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class UploadsApi {
  UploadsApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Abandon an upload and discard its staged bytes.
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] id (required):
  Future<Response> abandonUploadWithHttpInfo(String id, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/memos/uploads/{id}'
      .replaceAll('{id}', id);

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    const contentTypes = <String>[];


    return apiClient.invokeAPI(
      path,
      'DELETE',
      queryParams,
      postBody,
      headerParams,
      formParams,
      contentTypes.isEmpty ? null : contentTypes.first,
      abortTrigger: abortTrigger,
    );
  }

  /// Abandon an upload and discard its staged bytes.
  ///
  /// Parameters:
  ///
  /// * [String] id (required):
  Future<void> abandonUpload(String id, { Future<void>? abortTrigger, }) async {
    final response = await abandonUploadWithHttpInfo(id, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }

  /// Append a chunk.
  ///
  /// The body is raw bytes, not a form part and not base64 — that costs a third of the bytes and forces both ends to hold the whole thing in memory.  **A chunk must carry a `Content-Length`**; a chunked body is refused, because the offset arithmetic this endpoint is built on needs to know the length before it reads. The last chunk completing the declared size finalises the memo and returns it. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] id (required):
  ///
  /// * [int] uploadOffset (required):
  ///   The offset these bytes start at. A disagreement answers 409 carrying the server's.
  ///
  /// * [MultipartFile] body (required):
  Future<Response> appendChunkWithHttpInfo(String id, int uploadOffset, MultipartFile body, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/memos/uploads/{id}'
      .replaceAll('{id}', id);

    // ignore: prefer_final_locals
    Object? postBody = body;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    headerParams[r'Upload-Offset'] = parameterToString(uploadOffset);

    const contentTypes = <String>['application/octet-stream'];


    return apiClient.invokeAPI(
      path,
      'PATCH',
      queryParams,
      postBody,
      headerParams,
      formParams,
      contentTypes.isEmpty ? null : contentTypes.first,
      abortTrigger: abortTrigger,
    );
  }

  /// Append a chunk.
  ///
  /// The body is raw bytes, not a form part and not base64 — that costs a third of the bytes and forces both ends to hold the whole thing in memory.  **A chunk must carry a `Content-Length`**; a chunked body is refused, because the offset arithmetic this endpoint is built on needs to know the length before it reads. The last chunk completing the declared size finalises the memo and returns it. 
  ///
  /// Parameters:
  ///
  /// * [String] id (required):
  ///
  /// * [int] uploadOffset (required):
  ///   The offset these bytes start at. A disagreement answers 409 carrying the server's.
  ///
  /// * [MultipartFile] body (required):
  Future<UploadState?> appendChunk(String id, int uploadOffset, MultipartFile body, { Future<void>? abortTrigger, }) async {
    final response = await appendChunkWithHttpInfo(id, uploadOffset, body, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'UploadState',) as UploadState;
    
    }
    return null;
  }

  /// Where this upload got to.
  ///
  /// **This can finalise.** `Status` completes an upload whose bytes are all present, which is what makes a client's \"where did I get to\" call safe after a restart that landed the last chunk and died before committing — and it means this read answers the same refusals the append does. A client polling here needs the 409 and 422 branches as much as one sending bytes. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] id (required):
  Future<Response> getUploadWithHttpInfo(String id, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/memos/uploads/{id}'
      .replaceAll('{id}', id);

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    const contentTypes = <String>[];


    return apiClient.invokeAPI(
      path,
      'GET',
      queryParams,
      postBody,
      headerParams,
      formParams,
      contentTypes.isEmpty ? null : contentTypes.first,
      abortTrigger: abortTrigger,
    );
  }

  /// Where this upload got to.
  ///
  /// **This can finalise.** `Status` completes an upload whose bytes are all present, which is what makes a client's \"where did I get to\" call safe after a restart that landed the last chunk and died before committing — and it means this read answers the same refusals the append does. A client polling here needs the 409 and 422 branches as much as one sending bytes. 
  ///
  /// Parameters:
  ///
  /// * [String] id (required):
  Future<UploadState?> getUpload(String id, { Future<void>? abortTrigger, }) async {
    final response = await getUploadWithHttpInfo(id, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'UploadState',) as UploadState;
    
    }
    return null;
  }

  /// Declare an upload and open a session for it.
  ///
  /// `idempotency_key` is minted per capture and persisted by the client BEFORE the request goes out, so an HTTP retry is a replay rather than a second memo. A replay — same key, same declaration — answers `200` with the existing session; a mismatch answers `409` and the client mints a fresh key. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [OpenUploadRequest] openUploadRequest (required):
  Future<Response> openUploadWithHttpInfo(OpenUploadRequest openUploadRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/memos/uploads';

    // ignore: prefer_final_locals
    Object? postBody = openUploadRequest;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    const contentTypes = <String>['application/json'];


    return apiClient.invokeAPI(
      path,
      'POST',
      queryParams,
      postBody,
      headerParams,
      formParams,
      contentTypes.isEmpty ? null : contentTypes.first,
      abortTrigger: abortTrigger,
    );
  }

  /// Declare an upload and open a session for it.
  ///
  /// `idempotency_key` is minted per capture and persisted by the client BEFORE the request goes out, so an HTTP retry is a replay rather than a second memo. A replay — same key, same declaration — answers `200` with the existing session; a mismatch answers `409` and the client mints a fresh key. 
  ///
  /// Parameters:
  ///
  /// * [OpenUploadRequest] openUploadRequest (required):
  Future<UploadState?> openUpload(OpenUploadRequest openUploadRequest, { Future<void>? abortTrigger, }) async {
    final response = await openUploadWithHttpInfo(openUploadRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'UploadState',) as UploadState;
    
    }
    return null;
  }
}
