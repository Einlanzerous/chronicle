//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class ReferencesApi {
  ReferencesApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Resolve a batch of references into live cards.
  ///
  /// Takes the descriptors a note payload handed out and answers one resolution per descriptor, **keyed on `token`** — a token is unique within one scan's output, so a client aligns answers without depending on order. Order is preserved anyway.  **The server re-derives every descriptor from its `token`** (CHRN-97 ruling 8). `system`, `key`, `target` and `number` are parsed from the token under the same grammar that produced them, and a descriptor that disagrees with the grammar is refused with `400`. The one thing a client may not choose is which upstream gets dialled: this is the one code path in the design that leaves the process.  **Chronicle's own `CHR-` and `DSC-` references never leave the process.** They resolve against the notes and discussions tables — no network, no staleness. A soft-deleted note answers `broken` with `outcome: deleted`, no title and no URL.  **This endpoint parses tokens; it does not scan documents, and it never feeds the miss feed.** Whoever scans owns that — the note handler — and a second caller would make one dead key in one note log twice per page render.  At most 50 descriptors per call, which is CHRN-51's cap: a larger batch could never be checked in one render, so it is refused rather than silently truncated. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [ResolveRequest] resolveRequest (required):
  Future<Response> resolveReferencesWithHttpInfo(ResolveRequest resolveRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/references/resolve';

    // ignore: prefer_final_locals
    Object? postBody = resolveRequest;

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

  /// Resolve a batch of references into live cards.
  ///
  /// Takes the descriptors a note payload handed out and answers one resolution per descriptor, **keyed on `token`** — a token is unique within one scan's output, so a client aligns answers without depending on order. Order is preserved anyway.  **The server re-derives every descriptor from its `token`** (CHRN-97 ruling 8). `system`, `key`, `target` and `number` are parsed from the token under the same grammar that produced them, and a descriptor that disagrees with the grammar is refused with `400`. The one thing a client may not choose is which upstream gets dialled: this is the one code path in the design that leaves the process.  **Chronicle's own `CHR-` and `DSC-` references never leave the process.** They resolve against the notes and discussions tables — no network, no staleness. A soft-deleted note answers `broken` with `outcome: deleted`, no title and no URL.  **This endpoint parses tokens; it does not scan documents, and it never feeds the miss feed.** Whoever scans owns that — the note handler — and a second caller would make one dead key in one note log twice per page render.  At most 50 descriptors per call, which is CHRN-51's cap: a larger batch could never be checked in one render, so it is refused rather than silently truncated. 
  ///
  /// Parameters:
  ///
  /// * [ResolveRequest] resolveRequest (required):
  Future<ResolveResponse?> resolveReferences(ResolveRequest resolveRequest, { Future<void>? abortTrigger, }) async {
    final response = await resolveReferencesWithHttpInfo(resolveRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'ResolveResponse',) as ResolveResponse;
    
    }
    return null;
  }
}
