//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class DiscussionsApi {
  DiscussionsApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Put an account on a thread — a person, or the Scribe.
  ///
  /// Participation is who is EXPECTED TO READ, not who may write: nothing requires a turn's author to be a participant, and posting adds a person by itself. This is how an agent joins — an agent never posts first, so it is never added by posting. Re-adding somebody removed brings them back without reattributing the original invitation. The actor must be a person. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [ParticipantRequest] participantRequest (required):
  Future<Response> addParticipantWithHttpInfo(String ref, ParticipantRequest participantRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions/{ref}/participants'
      .replaceAll('{ref}', ref);

    // ignore: prefer_final_locals
    Object? postBody = participantRequest;

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

  /// Put an account on a thread — a person, or the Scribe.
  ///
  /// Participation is who is EXPECTED TO READ, not who may write: nothing requires a turn's author to be a participant, and posting adds a person by itself. This is how an agent joins — an agent never posts first, so it is never added by posting. Re-adding somebody removed brings them back without reattributing the original invitation. The actor must be a person. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [ParticipantRequest] participantRequest (required):
  Future<void> addParticipant(String ref, ParticipantRequest participantRequest, { Future<void>? abortTrigger, }) async {
    final response = await addParticipantWithHttpInfo(ref, participantRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }

  /// Reply.
  ///
  /// `seq` is assigned by the server under the thread's row lock; there is no field for it and no field for the author's kind, which the store derives from the account. `composed_at` is the client's claim about when this was written — a phone that was offline — and is carried, never sorted on.  An agent session may reply. What it may not do is reply to an agent: CH091 requires the immediately preceding turn to be a person's, and the refusal is the store's (`409 agent_after_agent`). A resolved thread takes no more turns (`409 discussion_resolved`); continue by opening a new thread that cites it.  Posting is reading: the author's marker moves to this turn. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [NewTurnRequest] newTurnRequest (required):
  Future<Response> appendTurnWithHttpInfo(String ref, NewTurnRequest newTurnRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions/{ref}/turns'
      .replaceAll('{ref}', ref);

    // ignore: prefer_final_locals
    Object? postBody = newTurnRequest;

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

  /// Reply.
  ///
  /// `seq` is assigned by the server under the thread's row lock; there is no field for it and no field for the author's kind, which the store derives from the account. `composed_at` is the client's claim about when this was written — a phone that was offline — and is carried, never sorted on.  An agent session may reply. What it may not do is reply to an agent: CH091 requires the immediately preceding turn to be a person's, and the refusal is the store's (`409 agent_after_agent`). A resolved thread takes no more turns (`409 discussion_resolved`); continue by opening a new thread that cites it.  Posting is reading: the author's marker moves to this turn. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [NewTurnRequest] newTurnRequest (required):
  Future<Turn?> appendTurn(String ref, NewTurnRequest newTurnRequest, { Future<void>? abortTrigger, }) async {
    final response = await appendTurnWithHttpInfo(ref, newTurnRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Turn',) as Turn;
    
    }
    return null;
  }

  /// A thread, in order, with its participants and your unread count.
  ///
  /// `turns` are in `seq` order and carry each author's kind AS IT WAS WHEN THE TURN WAS WRITTEN — frozen on the row, so a later account edit cannot rewrite who said what. `participants` are who is expected to read, current kind and current marker, removed ones included because their turns are still in the thread.  `unread` is the caller's: `max(seq) − last_read_seq`, computed by the store. Absent when the caller is not a participant (reading is not joining), and absent for an agent. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  Future<Response> getDiscussionWithHttpInfo(String ref, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions/{ref}'
      .replaceAll('{ref}', ref);

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

  /// A thread, in order, with its participants and your unread count.
  ///
  /// `turns` are in `seq` order and carry each author's kind AS IT WAS WHEN THE TURN WAS WRITTEN — frozen on the row, so a later account edit cannot rewrite who said what. `participants` are who is expected to read, current kind and current marker, removed ones included because their turns are still in the thread.  `unread` is the caller's: `max(seq) − last_read_seq`, computed by the store. Absent when the caller is not a participant (reading is not joining), and absent for an agent. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  Future<Thread?> getDiscussion(String ref, { Future<void>? abortTrigger, }) async {
    final response = await getDiscussionWithHttpInfo(ref, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Thread',) as Thread;
    
    }
    return null;
  }

  /// The threads filed against a page.
  ///
  /// Resolved threads are INCLUDED — a resolved thread concluded, which is the most interesting thing a thread can do, and hiding it is what CHRN-46's third `Done when` forbids. Unfiled threads have no page and are not listed here; a client reaches them by ref, or through the unread badge. A page-independent list needs a store query this ticket does not add. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] page (required):
  ///   A page path, `estate/conventions/naming`. A redirect left by a move is followed.
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  ///
  /// * [String] cursor:
  ///   Opaque; the `next_cursor` of the previous page. Absent means the start. Never an offset: every list here is over an append-only table, and an offset silently repeats and skips rows as new ones land. 
  Future<Response> listDiscussionsWithHttpInfo(String page, { int? limit, String? cursor, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions';

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

      queryParams.addAll(_queryParams('', 'page', page));
    if (limit != null) {
      queryParams.addAll(_queryParams('', 'limit', limit));
    }
    if (cursor != null) {
      queryParams.addAll(_queryParams('', 'cursor', cursor));
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

  /// The threads filed against a page.
  ///
  /// Resolved threads are INCLUDED — a resolved thread concluded, which is the most interesting thing a thread can do, and hiding it is what CHRN-46's third `Done when` forbids. Unfiled threads have no page and are not listed here; a client reaches them by ref, or through the unread badge. A page-independent list needs a store query this ticket does not add. 
  ///
  /// Parameters:
  ///
  /// * [String] page (required):
  ///   A page path, `estate/conventions/naming`. A redirect left by a move is followed.
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  ///
  /// * [String] cursor:
  ///   Opaque; the `next_cursor` of the previous page. Absent means the start. Never an offset: every list here is over an append-only table, and an offset silently repeats and skips rows as new ones land. 
  Future<DiscussionList?> listDiscussions(String page, { int? limit, String? cursor, Future<void>? abortTrigger, }) async {
    final response = await listDiscussionsWithHttpInfo(page, limit: limit, cursor: cursor, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'DiscussionList',) as DiscussionList;
    
    }
    return null;
  }

  /// The badge — every thread this account has something unread in.
  ///
  /// Threads with nothing unread are omitted rather than reported as 0, so the length is the number wanting attention. Removed participants are excluded: being taken off a thread is exactly a statement that it is no longer yours to read. An agent gets an empty list — an agent has no unread, by CHRN-45 ruling 4, because unread answers \"what should I look at\" and an agent that scanned it would reply to everything. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> listUnreadWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions/unread';

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

  /// The badge — every thread this account has something unread in.
  ///
  /// Threads with nothing unread are omitted rather than reported as 0, so the length is the number wanting attention. Removed participants are excluded: being taken off a thread is exactly a statement that it is no longer yours to read. An agent gets an empty list — an agent has no unread, by CHRN-45 ruling 4, because unread answers \"what should I look at\" and an agent that scanned it would reply to everything. 
  Future<UnreadList?> listUnread({ Future<void>? abortTrigger, }) async {
    final response = await listUnreadWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'UnreadList',) as UnreadList;
    
    }
    return null;
  }

  /// Report how far you have read.
  ///
  /// A POSITION, not \"mark everything read\": the client says which turn it has read through, which is what makes the call idempotent and safe to retry. The marker only moves forward — a stale report from a slow device is a no-op, not an error — and is clamped to the thread's last turn, so a client one off against a stale list cannot park itself in the future and read 0 unread until the thread catches up.  Reading is not joining: a caller with no participant row is refused (`403 not_a_participant`). An agent carries no marker at all (`403 agent_has_no_marker`). 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [MarkReadRequest] markReadRequest (required):
  Future<Response> markReadWithHttpInfo(String ref, MarkReadRequest markReadRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions/{ref}/read'
      .replaceAll('{ref}', ref);

    // ignore: prefer_final_locals
    Object? postBody = markReadRequest;

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

  /// Report how far you have read.
  ///
  /// A POSITION, not \"mark everything read\": the client says which turn it has read through, which is what makes the call idempotent and safe to retry. The marker only moves forward — a stale report from a slow device is a no-op, not an error — and is clamped to the thread's last turn, so a client one off against a stale list cannot park itself in the future and read 0 unread until the thread catches up.  Reading is not joining: a caller with no participant row is refused (`403 not_a_participant`). An agent carries no marker at all (`403 agent_has_no_marker`). 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [MarkReadRequest] markReadRequest (required):
  Future<void> markRead(String ref, MarkReadRequest markReadRequest, { Future<void>? abortTrigger, }) async {
    final response = await markReadWithHttpInfo(ref, markReadRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }

  /// Open a thread with its first turn.
  ///
  /// The thread and turn 1 are written together: a thread with no turns is a state no reader should have to handle. The session's account authors turn 1, and that is where \"an agent cannot open a thread\" is enforced — by the store, which refuses an agent at seq 1 because no person's turn precedes it (`409 agent_after_agent`).  Posting is reading: the opener's marker starts at their own turn. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [NewDiscussionRequest] newDiscussionRequest (required):
  Future<Response> openDiscussionWithHttpInfo(NewDiscussionRequest newDiscussionRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions';

    // ignore: prefer_final_locals
    Object? postBody = newDiscussionRequest;

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

  /// Open a thread with its first turn.
  ///
  /// The thread and turn 1 are written together: a thread with no turns is a state no reader should have to handle. The session's account authors turn 1, and that is where \"an agent cannot open a thread\" is enforced — by the store, which refuses an agent at seq 1 because no person's turn precedes it (`409 agent_after_agent`).  Posting is reading: the opener's marker starts at their own turn. 
  ///
  /// Parameters:
  ///
  /// * [NewDiscussionRequest] newDiscussionRequest (required):
  Future<Thread?> openDiscussion(NewDiscussionRequest newDiscussionRequest, { Future<void>? abortTrigger, }) async {
    final response = await openDiscussionWithHttpInfo(newDiscussionRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Thread',) as Thread;
    
    }
    return null;
  }

  /// Take an account off a thread, without touching a word they said.
  ///
  /// A state change, not a deletion: every turn carries its own author. Idempotent — an account never on the thread, or already off it, answers the same 204. A removed person may still post, and their removal stands; only this operation's inverse puts them back. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [String] id (required):
  Future<Response> removeParticipantWithHttpInfo(String ref, String id, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions/{ref}/participants/{id}'
      .replaceAll('{ref}', ref)
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

  /// Take an account off a thread, without touching a word they said.
  ///
  /// A state change, not a deletion: every turn carries its own author. Idempotent — an account never on the thread, or already off it, answers the same 204. A removed person may still post, and their removal stands; only this operation's inverse puts them back. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [String] id (required):
  Future<void> removeParticipant(String ref, String id, { Future<void>? abortTrigger, }) async {
    final response = await removeParticipantWithHttpInfo(ref, id, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }

  /// Resolve a thread — into a new note, an existing one, or nothing.
  ///
  /// A thread ends by producing something durable, and the link runs both ways: the thread names the note, and the note names every thread that concluded into it. `into` is REQUIRED and has no default, because resolving without a note is allowed but must be a deliberate choice rather than the path of least resistance.  - `new_note` writes the note and the link in one transaction. - `existing_note` APPENDS a revision to a note that exists — the   ordinary case: \"Resolved into PRINCIPLES §6\" is a section of a   long-lived document. - `nothing` records the conclusion with no note. It can be completed   later by resolving again with a note; the original resolver and   instant stand.  The resolver is the session's person — an agent cannot write what a conversation concluded (`403 person_required`). A recorded resolution is never rewritten: resolving into a DIFFERENT note answers `409 resolution_fixed`. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [ResolveDiscussionRequest] resolveDiscussionRequest (required):
  Future<Response> resolveDiscussionWithHttpInfo(String ref, ResolveDiscussionRequest resolveDiscussionRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/discussions/{ref}/resolve'
      .replaceAll('{ref}', ref);

    // ignore: prefer_final_locals
    Object? postBody = resolveDiscussionRequest;

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

  /// Resolve a thread — into a new note, an existing one, or nothing.
  ///
  /// A thread ends by producing something durable, and the link runs both ways: the thread names the note, and the note names every thread that concluded into it. `into` is REQUIRED and has no default, because resolving without a note is allowed but must be a deliberate choice rather than the path of least resistance.  - `new_note` writes the note and the link in one transaction. - `existing_note` APPENDS a revision to a note that exists — the   ordinary case: \"Resolved into PRINCIPLES §6\" is a section of a   long-lived document. - `nothing` records the conclusion with no note. It can be completed   later by resolving again with a note; the original resolver and   instant stand.  The resolver is the session's person — an agent cannot write what a conversation concluded (`403 person_required`). A recorded resolution is never rewritten: resolving into a DIFFERENT note answers `409 resolution_fixed`. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A discussion reference, parsed leniently — `DSC-0007`, `dsc-7`, `DSC-00007` all name thread 7 — and rendered strictly as `DSC-0007`.
  ///
  /// * [ResolveDiscussionRequest] resolveDiscussionRequest (required):
  Future<DiscussionResolution?> resolveDiscussion(String ref, ResolveDiscussionRequest resolveDiscussionRequest, { Future<void>? abortTrigger, }) async {
    final response = await resolveDiscussionWithHttpInfo(ref, resolveDiscussionRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'DiscussionResolution',) as DiscussionResolution;
    
    }
    return null;
  }
}
