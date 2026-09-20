//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class MemosApi {
  MemosApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// The recording itself, while it still exists.
  ///
  /// `http.ServeContent` serves it, so `Range`, `206`, `If-Range`, `If-None-Match` and `If-Modified-Since` all work and the range arithmetic is the standard library's rather than this handler's — the last thing to hand-roll in the one operation that serves irreplaceable bytes. Two of its inputs are chosen rather than defaulted:  - **the validator is the memo id**, quoted. `0003`'s `CH002` refuses any   UPDATE that moves `author_id`, `content_hash`, `byte_size` or   `captured_at`, so one id names one byte sequence for as long as the   row exists. The `content_hash` would buy no extra precision and would   publish half the storage path. - **the modification time is `captured_at`**, not the file's mtime,   which moves when a finished upload is renamed into place, when a   restore from backup rewrites it, and when CHRN-68's drill runs. A   validator that changes without the content changing is a cache that   misses for no reason.  `Cache-Control: private, no-cache` is on the response and is load bearing. The default is not \"no caching\" but HEURISTIC freshness: with a strong validator and a `Last-Modified`, RFC 9111 §4.2.2 invites a browser to treat a memo captured 25 days ago as fresh for two and a half days, inside which a player serves cached bytes without asking — never seeing the `410` after a sweep, or the `404` after an access change. One conditional request buys a `304` instead.  **A pruned memo answers `410`, never `404`.** The recording was deleted by policy and the transcript remains; a client holding a link must be able to tell that from *no such memo*, which is the same distinction a withdrawn note's `410` draws. The body is the shared `Error` with code `audio_pruned` and nothing more: the date and the surviving transcript are on `MemoProvenance`, which is what a client already holds and the only thing an `<audio src>` element could ever render from.  **A memo whose row expects its audio and whose file is absent answers `500`, code `audio_missing`, and leaves an `ERROR` line** whether or not anybody reads the response. That is CHRN-23's `missing`: not an expected absence, but the failure the reconciliation report exists to surface, and `storage.go` already logs the same condition unconditionally.  **The bytes, so the memo's author and the owner only** — `getMemoTranscript`'s rule and its reason. A member who may not ask can tell from `MemoProvenance.audio_readable` rather than from a request that fails. The comparison runs BEFORE any retention state is read and before the file is stat'd, so neither a refusal nor a log line can say anything about another account's recording.  A multi-range request is not served: a `Range` header containing a comma is dropped before `ServeContent` sees it and the whole body is answered, which RFC 9110 permits and which no media element asks for. So `multipart/byteranges` is never produced and is not declared. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] memoId (required):
  ///   A memo's id. `RevisionMeta.memo_id` is where a client gets one, and for a member who is not the author it is the ONLY place: `SearchHit.memo_id` and the transcription report are owner-only, and `BatchItem.memo_id` is scoped to the author. 
  ///
  /// * [String] range:
  ///   One byte range, `bytes=0-65535`. A header naming more than one — a comma — is IGNORED and the whole body answered, so no response here is `multipart/byteranges`. 
  ///
  /// * [String] ifNoneMatch:
  ///   The `ETag` of a previous read — the memo id, quoted. A match answers `304`.
  Future<Response> getMemoAudioWithHttpInfo(String memoId, { String? range, String? ifNoneMatch, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/audio/{memo_id}'
      .replaceAll('{memo_id}', memoId);

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    if (range != null) {
      headerParams[r'Range'] = parameterToString(range);
    }
    if (ifNoneMatch != null) {
      headerParams[r'If-None-Match'] = parameterToString(ifNoneMatch);
    }

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

  /// The recording itself, while it still exists.
  ///
  /// `http.ServeContent` serves it, so `Range`, `206`, `If-Range`, `If-None-Match` and `If-Modified-Since` all work and the range arithmetic is the standard library's rather than this handler's — the last thing to hand-roll in the one operation that serves irreplaceable bytes. Two of its inputs are chosen rather than defaulted:  - **the validator is the memo id**, quoted. `0003`'s `CH002` refuses any   UPDATE that moves `author_id`, `content_hash`, `byte_size` or   `captured_at`, so one id names one byte sequence for as long as the   row exists. The `content_hash` would buy no extra precision and would   publish half the storage path. - **the modification time is `captured_at`**, not the file's mtime,   which moves when a finished upload is renamed into place, when a   restore from backup rewrites it, and when CHRN-68's drill runs. A   validator that changes without the content changing is a cache that   misses for no reason.  `Cache-Control: private, no-cache` is on the response and is load bearing. The default is not \"no caching\" but HEURISTIC freshness: with a strong validator and a `Last-Modified`, RFC 9111 §4.2.2 invites a browser to treat a memo captured 25 days ago as fresh for two and a half days, inside which a player serves cached bytes without asking — never seeing the `410` after a sweep, or the `404` after an access change. One conditional request buys a `304` instead.  **A pruned memo answers `410`, never `404`.** The recording was deleted by policy and the transcript remains; a client holding a link must be able to tell that from *no such memo*, which is the same distinction a withdrawn note's `410` draws. The body is the shared `Error` with code `audio_pruned` and nothing more: the date and the surviving transcript are on `MemoProvenance`, which is what a client already holds and the only thing an `<audio src>` element could ever render from.  **A memo whose row expects its audio and whose file is absent answers `500`, code `audio_missing`, and leaves an `ERROR` line** whether or not anybody reads the response. That is CHRN-23's `missing`: not an expected absence, but the failure the reconciliation report exists to surface, and `storage.go` already logs the same condition unconditionally.  **The bytes, so the memo's author and the owner only** — `getMemoTranscript`'s rule and its reason. A member who may not ask can tell from `MemoProvenance.audio_readable` rather than from a request that fails. The comparison runs BEFORE any retention state is read and before the file is stat'd, so neither a refusal nor a log line can say anything about another account's recording.  A multi-range request is not served: a `Range` header containing a comma is dropped before `ServeContent` sees it and the whole body is answered, which RFC 9110 permits and which no media element asks for. So `multipart/byteranges` is never produced and is not declared. 
  ///
  /// Parameters:
  ///
  /// * [String] memoId (required):
  ///   A memo's id. `RevisionMeta.memo_id` is where a client gets one, and for a member who is not the author it is the ONLY place: `SearchHit.memo_id` and the transcription report are owner-only, and `BatchItem.memo_id` is scoped to the author. 
  ///
  /// * [String] range:
  ///   One byte range, `bytes=0-65535`. A header naming more than one — a comma — is IGNORED and the whole body answered, so no response here is `multipart/byteranges`. 
  ///
  /// * [String] ifNoneMatch:
  ///   The `ETag` of a previous read — the memo id, quoted. A match answers `304`.
  Future<void> getMemoAudio(String memoId, { String? range, String? ifNoneMatch, Future<void>? abortTrigger, }) async {
    final response = await getMemoAudioWithHttpInfo(memoId, range: range, ifNoneMatch: ifNoneMatch, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }

  /// What a memo said, in full.
  ///
  /// **Tier 2, authored by nobody and rebuildable by nobody.** The audio is pruned at thirty days and this outlives it — the asymmetry the whole retention design rests on — which is also why nothing here carries a retention state: a transcript does not prune.  **The row is `GetTranscript`'s**: the newest COMPLETE transcript, or the newest partial one when no complete transcript exists yet. A memo can hold several rows — a `retranscribe` with a different model writes another — and `partial` says which kind this one is. It is a fact the ASR service recorded about its own run, carried across unchanged and never computed here.  **The words, so the memo's author and the owner only.** `search` is owner-only in this document because \"the transcript half spans every author's memos\"; the same sentence applies to one named transcript, and the distinction is the document's own: *what a person decided to write down and what they happened to say into a phone are different facts*. Another account's memo answers `404`, byte-identical to an id that names nothing. A member who may not read it can tell from `MemoProvenance.transcript.readable` without making the request.  A readable memo with no transcript row yet answers `404` with code `no_transcript` rather than `not_found`: the remedy is to wait, not to correct the id. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] memoId (required):
  ///   A memo's id. `RevisionMeta.memo_id` is where a client gets one, and for a member who is not the author it is the ONLY place: `SearchHit.memo_id` and the transcription report are owner-only, and `BatchItem.memo_id` is scoped to the author. 
  Future<Response> getMemoTranscriptWithHttpInfo(String memoId, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/transcripts/{memo_id}'
      .replaceAll('{memo_id}', memoId);

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

  /// What a memo said, in full.
  ///
  /// **Tier 2, authored by nobody and rebuildable by nobody.** The audio is pruned at thirty days and this outlives it — the asymmetry the whole retention design rests on — which is also why nothing here carries a retention state: a transcript does not prune.  **The row is `GetTranscript`'s**: the newest COMPLETE transcript, or the newest partial one when no complete transcript exists yet. A memo can hold several rows — a `retranscribe` with a different model writes another — and `partial` says which kind this one is. It is a fact the ASR service recorded about its own run, carried across unchanged and never computed here.  **The words, so the memo's author and the owner only.** `search` is owner-only in this document because \"the transcript half spans every author's memos\"; the same sentence applies to one named transcript, and the distinction is the document's own: *what a person decided to write down and what they happened to say into a phone are different facts*. Another account's memo answers `404`, byte-identical to an id that names nothing. A member who may not read it can tell from `MemoProvenance.transcript.readable` without making the request.  A readable memo with no transcript row yet answers `404` with code `no_transcript` rather than `not_found`: the remedy is to wait, not to correct the id. 
  ///
  /// Parameters:
  ///
  /// * [String] memoId (required):
  ///   A memo's id. `RevisionMeta.memo_id` is where a client gets one, and for a member who is not the author it is the ONLY place: `SearchHit.memo_id` and the transcription report are owner-only, and `BatchItem.memo_id` is scoped to the author. 
  Future<MemoTranscript?> getMemoTranscript(String memoId, { Future<void>? abortTrigger, }) async {
    final response = await getMemoTranscriptWithHttpInfo(memoId, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'MemoTranscript',) as MemoTranscript;
    
    }
    return null;
  }
}
