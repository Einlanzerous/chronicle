//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class MetaApi {
  MetaApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Liveness. No dependencies, no credential.
  ///
  /// Answers if the process is running. Deliberately free of dependencies: a database that is down is not a reason to restart the binary.  Load-bearing beyond liveness — CHRN-59's QR onboarding probes this to check a server address before any credential exists, which is also why it keeps an unprefixed path. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> getHealthzWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/healthz';

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

  /// Liveness. No dependencies, no credential.
  ///
  /// Answers if the process is running. Deliberately free of dependencies: a database that is down is not a reason to restart the binary.  Load-bearing beyond liveness — CHRN-59's QR onboarding probes this to check a server address before any credential exists, which is also why it keeps an unprefixed path. 
  Future<Health?> getHealthz({ Future<void>? abortTrigger, }) async {
    final response = await getHealthzWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Health',) as Health;
    
    }
    return null;
  }

  /// Readiness. Pings the database.
  ///
  /// Answers whether this process can serve traffic. A load balancer takes an unready instance out of rotation; it does not kill it, which is why this is a different question from liveness and a different route. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> getReadyzWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/readyz';

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

  /// Readiness. Pings the database.
  ///
  /// Answers whether this process can serve traffic. A load balancer takes an unready instance out of rotation; it does not kill it, which is why this is a different question from liveness and a different route. 
  Future<Readiness?> getReadyz({ Future<void>? abortTrigger, }) async {
    final response = await getReadyzWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Readiness',) as Readiness;
    
    }
    return null;
  }
}
