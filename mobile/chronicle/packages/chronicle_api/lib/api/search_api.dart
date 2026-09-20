//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class SearchApi {
  SearchApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Full-text search across notes and transcripts.
  ///
  /// **Not a list.** Rank-ordered, and the ranks move as the corpus moves, so a cursor over this order would be a lie with a shape. It takes `limit`, answers the top N, and carries no cursor.  Transcripts are searched as well as notes because audio is pruned at thirty days: from day thirty-one the transcript is the only account of what somebody said, and a search over notes alone would find the small fraction of the corpus that got triaged. Each hit says which it is — what a person decided to write down and what they happened to say into a phone are different facts.  The query language is Postgres `websearch_to_tsquery`: bare words are ANDed, \"quoted phrases\" are phrases, `OR` is OR, a leading `-` excludes.  **Owner only, because the transcript half spans every author's memos** — including ones never triaged — and the store's query takes no actor. This API refuses that elsewhere: \"a list that merely hides a memo is not access control\", which is why `GET /admin/triage` is owner too. The same rule holds here rather than being crossed unstated. A member-visible search scoped to the caller's own transcripts beside the shared notes is a store query this ticket raised rather than added. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] q (required):
  ///   The query. One made only of punctuation matches nothing and is refused as an empty question rather than answered as an empty corpus.
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  Future<Response> searchWithHttpInfo(String q, { int? limit, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/search';

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

      queryParams.addAll(_queryParams('', 'q', q));
    if (limit != null) {
      queryParams.addAll(_queryParams('', 'limit', limit));
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

  /// Full-text search across notes and transcripts.
  ///
  /// **Not a list.** Rank-ordered, and the ranks move as the corpus moves, so a cursor over this order would be a lie with a shape. It takes `limit`, answers the top N, and carries no cursor.  Transcripts are searched as well as notes because audio is pruned at thirty days: from day thirty-one the transcript is the only account of what somebody said, and a search over notes alone would find the small fraction of the corpus that got triaged. Each hit says which it is — what a person decided to write down and what they happened to say into a phone are different facts.  The query language is Postgres `websearch_to_tsquery`: bare words are ANDed, \"quoted phrases\" are phrases, `OR` is OR, a leading `-` excludes.  **Owner only, because the transcript half spans every author's memos** — including ones never triaged — and the store's query takes no actor. This API refuses that elsewhere: \"a list that merely hides a memo is not access control\", which is why `GET /admin/triage` is owner too. The same rule holds here rather than being crossed unstated. A member-visible search scoped to the caller's own transcripts beside the shared notes is a store query this ticket raised rather than added. 
  ///
  /// Parameters:
  ///
  /// * [String] q (required):
  ///   The query. One made only of punctuation matches nothing and is refused as an empty question rather than answered as an empty corpus.
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  Future<SearchResults?> search(String q, { int? limit, Future<void>? abortTrigger, }) async {
    final response = await searchWithHttpInfo(q, limit: limit, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'SearchResults',) as SearchResults;
    
    }
    return null;
  }
}
