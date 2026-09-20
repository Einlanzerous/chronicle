//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class StorageApi {
  StorageApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// What the corpus costs, and whether the disk agrees with the database.
  ///
  /// CHRN-23's *\"a number the service reports rather than one someone runs du for\"*.  **A read.** It reports orphans; it never deletes one and never hands the pruner a list. Orphans are reported so a person can decide, which is a different thing from a job acting on them.  Counts are exact. The `*_sample` lists are bounded, so a corpus-wide problem produces a readable answer rather than a megabyte of JSON. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> getStorageReportWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/admin/storage';

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

  /// What the corpus costs, and whether the disk agrees with the database.
  ///
  /// CHRN-23's *\"a number the service reports rather than one someone runs du for\"*.  **A read.** It reports orphans; it never deletes one and never hands the pruner a list. Orphans are reported so a person can decide, which is a different thing from a job acting on them.  Counts are exact. The `*_sample` lists are bounded, so a corpus-wide problem produces a readable answer rather than a megabyte of JSON. 
  Future<StorageReport?> getStorageReport({ Future<void>? abortTrigger, }) async {
    final response = await getStorageReportWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'StorageReport',) as StorageReport;
    
    }
    return null;
  }
}
