//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class TriageApi {
  TriageApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Confirm a batch of decisions.
  ///
  /// **This is the write.** Each item carries the `generation` it was decided against, so a proposal that has been regenerated since the client read it is refused rather than confirmed against text nobody saw.  There is no batch-wide status and there must not be: the interesting case is item 7 of 12 failing, and a single status could not say which one to re-show. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [AcceptRequest] acceptRequest (required):
  Future<Response> acceptTriageWithHttpInfo(AcceptRequest acceptRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/triage/accept';

    // ignore: prefer_final_locals
    Object? postBody = acceptRequest;

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

  /// Confirm a batch of decisions.
  ///
  /// **This is the write.** Each item carries the `generation` it was decided against, so a proposal that has been regenerated since the client read it is refused rather than confirmed against text nobody saw.  There is no batch-wide status and there must not be: the interesting case is item 7 of 12 failing, and a single status could not say which one to re-show. 
  ///
  /// Parameters:
  ///
  /// * [AcceptRequest] acceptRequest (required):
  Future<TriageResults?> acceptTriage(AcceptRequest acceptRequest, { Future<void>? abortTrigger, }) async {
    final response = await acceptTriageWithHttpInfo(acceptRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'TriageResults',) as TriageResults;
    
    }
    return null;
  }

  /// The next memos awaiting a decision, with their proposals.
  ///
  /// Batch-first because the real pattern is an evening pass over a day's memos, not a notification per capture. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  Future<Response> getTriageBatchWithHttpInfo({ int? limit, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/triage/batch';

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

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

  /// The next memos awaiting a decision, with their proposals.
  ///
  /// Batch-first because the real pattern is an evening pass over a day's memos, not a notification per capture. 
  ///
  /// Parameters:
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  Future<TriageBatch?> getTriageBatch({ int? limit, Future<void>? abortTrigger, }) async {
    final response = await getTriageBatchWithHttpInfo(limit: limit, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'TriageBatch',) as TriageBatch;
    
    }
    return null;
  }

  /// What triage left behind — the backlog by age, and the decisions that did not finish landing.
  ///
  /// Owner only, because it spans every author's corpus.  The four link states are the point. A decision writes tier 2 and then reaches Switchyard, and every gap between those two is a state a person has to be able to see: `in_flight` is still going, `unresolved` never arrived, `ambiguous` found more than one candidate, and `refused` was answered no. None of them is a fault the service can fix on its own, which is why they are reported rather than retried silently. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> getTriageReportWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/admin/triage';

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

  /// What triage left behind — the backlog by age, and the decisions that did not finish landing.
  ///
  /// Owner only, because it spans every author's corpus.  The four link states are the point. A decision writes tier 2 and then reaches Switchyard, and every gap between those two is a state a person has to be able to see: `in_flight` is still going, `unresolved` never arrived, `ambiguous` found more than one candidate, and `refused` was answered no. None of them is a fault the service can fix on its own, which is why they are reported rather than retried silently. 
  Future<TriageReport?> getTriageReport({ Future<void>? abortTrigger, }) async {
    final response = await getTriageReportWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'TriageReport',) as TriageReport;
    
    }
    return null;
  }

  /// Defer a memo — \"not now\".
  ///
  /// CHRN-34's escape. Most deferrals are \"not now\"; the ones that are not are the ones still legible in three weeks, which is why `reason` exists and is optional.  Holding somebody else's memo answers exactly as holding one that does not exist. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [HoldRequest] holdRequest (required):
  Future<Response> holdMemoWithHttpInfo(HoldRequest holdRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/triage/hold';

    // ignore: prefer_final_locals
    Object? postBody = holdRequest;

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

  /// Defer a memo — \"not now\".
  ///
  /// CHRN-34's escape. Most deferrals are \"not now\"; the ones that are not are the ones still legible in three weeks, which is why `reason` exists and is optional.  Holding somebody else's memo answers exactly as holding one that does not exist. 
  ///
  /// Parameters:
  ///
  /// * [HoldRequest] holdRequest (required):
  Future<DeferredItem?> holdMemo(HoldRequest holdRequest, { Future<void>? abortTrigger, }) async {
    final response = await holdMemoWithHttpInfo(holdRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'DeferredItem',) as DeferredItem;
    
    }
    return null;
  }

  /// What this account has deferred, oldest first.
  ///
  /// Carries `age_seconds` rather than only `held_at`, because the question being asked is \"how long has this been waiting\" and a client computing it from two clocks would get a different answer from the server's. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  Future<Response> listDeferredWithHttpInfo({ int? limit, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/triage/deferred';

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

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

  /// What this account has deferred, oldest first.
  ///
  /// Carries `age_seconds` rather than only `held_at`, because the question being asked is \"how long has this been waiting\" and a client computing it from two clocks would get a different answer from the server's. 
  ///
  /// Parameters:
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  Future<DeferredList?> listDeferred({ int? limit, Future<void>? abortTrigger, }) async {
    final response = await listDeferredWithHttpInfo(limit: limit, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'DeferredList',) as DeferredList;
    
    }
    return null;
  }

  /// Bring a deferred memo back into the batch.
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [ReleaseRequest] releaseRequest (required):
  Future<Response> releaseMemoWithHttpInfo(ReleaseRequest releaseRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/triage/release';

    // ignore: prefer_final_locals
    Object? postBody = releaseRequest;

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

  /// Bring a deferred memo back into the batch.
  ///
  /// Parameters:
  ///
  /// * [ReleaseRequest] releaseRequest (required):
  Future<void> releaseMemo(ReleaseRequest releaseRequest, { Future<void>? abortTrigger, }) async {
    final response = await releaseMemoWithHttpInfo(releaseRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }
}
