//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class TranscriptionApi {
  TranscriptionApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// How transcription is going, and which memos are stuck.
  ///
  /// The half of CHRN-27's `Done when` a test cannot answer: *\"a transcription failure leaves the memo in a state a human can see and retry.\"* A failure nobody can see is one nobody retries.  **A read.** The retry is `chronicle retranscribe`, on the host, because re-running transcription costs GPU time on a device three services share and that is not an unmetered HTTP verb until CHRN-26 has leased it. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> getTranscriptionReportWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/admin/transcription';

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

  /// How transcription is going, and which memos are stuck.
  ///
  /// The half of CHRN-27's `Done when` a test cannot answer: *\"a transcription failure leaves the memo in a state a human can see and retry.\"* A failure nobody can see is one nobody retries.  **A read.** The retry is `chronicle retranscribe`, on the host, because re-running transcription costs GPU time on a device three services share and that is not an unmetered HTTP verb until CHRN-26 has leased it. 
  Future<TranscriptionReport?> getTranscriptionReport({ Future<void>? abortTrigger, }) async {
    final response = await getTranscriptionReportWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'TranscriptionReport',) as TranscriptionReport;
    
    }
    return null;
  }
}
