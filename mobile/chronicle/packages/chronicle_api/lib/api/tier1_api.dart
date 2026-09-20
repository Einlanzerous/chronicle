//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class Tier1Api {
  Tier1Api([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// One page of the generated estate wiki, rendered.
  ///
  /// `body` is the page's markdown with the generator's front matter removed; `html` is that body rendered by the same renderer notes use, so the two tiers look alike on the page and differ in their marking.  A path the corpus could not hold — `..`, a leading slash, a dot-prefixed segment — is refused with `400` before touching disk. A path it merely does not have is `404`. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] path (required):
  ///   A page of the generated corpus, by the path its generator wrote it at and without the `.md` — `services/chronicle`, `versions`. Relative, slash-separated, and no segment may be empty or start with a dot. 
  Future<Response> getTier1PageWithHttpInfo(String path, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/tier1/page';

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

      queryParams.addAll(_queryParams('', 'path', path));

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

  /// One page of the generated estate wiki, rendered.
  ///
  /// `body` is the page's markdown with the generator's front matter removed; `html` is that body rendered by the same renderer notes use, so the two tiers look alike on the page and differ in their marking.  A path the corpus could not hold — `..`, a leading slash, a dot-prefixed segment — is refused with `400` before touching disk. A path it merely does not have is `404`. 
  ///
  /// Parameters:
  ///
  /// * [String] path (required):
  ///   A page of the generated corpus, by the path its generator wrote it at and without the `.md` — `services/chronicle`, `versions`. Relative, slash-separated, and no segment may be empty or start with a dot. 
  Future<Tier1Page?> getTier1Page(String path, { Future<void>? abortTrigger, }) async {
    final response = await getTier1PageWithHttpInfo(path, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Tier1Page',) as Tier1Page;
    
    }
    return null;
  }

  /// Every page of the generated estate wiki.
  ///
  /// The whole corpus, sorted by path, each page with its title. Not paginated: the corpus is bounded by the estate's size, not by what people write, and a client rendering a tree wants all of it.  Empty is a legitimate answer on a host the wiki has not yet been generated on. `generated.ref` and `generated.generated_at` are absent then, and on any corpus the generator wrote without a `build.json`. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> listTier1PagesWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/tier1/pages';

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

  /// Every page of the generated estate wiki.
  ///
  /// The whole corpus, sorted by path, each page with its title. Not paginated: the corpus is bounded by the estate's size, not by what people write, and a client rendering a tree wants all of it.  Empty is a legitimate answer on a host the wiki has not yet been generated on. `generated.ref` and `generated.generated_at` are absent then, and on any corpus the generator wrote without a `build.json`. 
  Future<Tier1PageList?> listTier1Pages({ Future<void>? abortTrigger, }) async {
    final response = await listTier1PagesWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Tier1PageList',) as Tier1PageList;
    
    }
    return null;
  }
}
