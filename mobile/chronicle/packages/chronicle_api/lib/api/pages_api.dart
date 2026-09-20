//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class PagesApi {
  PagesApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Create a page at a path.
  ///
  /// The parent must already exist and is resolved WITHOUT following redirects: a caller writing to a path needs to know it is writing where it thinks it is. A single-segment path creates a root page.  Here because nothing else creates one: a note must be filed on a page, and until this operation a fresh deployment had no page to file it on.  An agent session may create a page. A page is a container and carries no authored text and no confirming person; the person-only rule (CH041) is about revisions, and it holds where they are written. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [NewPageRequest] newPageRequest (required):
  Future<Response> createPageWithHttpInfo(NewPageRequest newPageRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/pages';

    // ignore: prefer_final_locals
    Object? postBody = newPageRequest;

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

  /// Create a page at a path.
  ///
  /// The parent must already exist and is resolved WITHOUT following redirects: a caller writing to a path needs to know it is writing where it thinks it is. A single-segment path creates a root page.  Here because nothing else creates one: a note must be filed on a page, and until this operation a fresh deployment had no page to file it on.  An agent session may create a page. A page is a container and carries no authored text and no confirming person; the person-only rule (CH041) is about revisions, and it holds where they are written. 
  ///
  /// Parameters:
  ///
  /// * [NewPageRequest] newPageRequest (required):
  Future<Page?> createPage(NewPageRequest newPageRequest, { Future<void>? abortTrigger, }) async {
    final response = await createPageWithHttpInfo(newPageRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Page',) as Page;
    
    }
    return null;
  }

  /// The whole page tree, as sorted paths.
  ///
  /// Every page's current path, sorted. A tree is a property of the paths — `estate` is the parent of `estate/conventions` — so this is the tree, and a client renders it however it likes. Bounded by the corpus, which is a few hundred pages at most, so it is not paginated. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> listPagesWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/pages';

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

  /// The whole page tree, as sorted paths.
  ///
  /// Every page's current path, sorted. A tree is a property of the paths — `estate` is the parent of `estate/conventions` — so this is the tree, and a client renders it however it likes. Bounded by the corpus, which is a few hundred pages at most, so it is not paginated. 
  Future<PageTree?> listPages({ Future<void>? abortTrigger, }) async {
    final response = await listPagesWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'PageTree',) as PageTree;
    
    }
    return null;
  }
}
