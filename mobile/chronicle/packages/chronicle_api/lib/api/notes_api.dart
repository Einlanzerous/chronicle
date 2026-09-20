//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class NotesApi {
  NotesApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Append a revision.
  ///
  /// **Always appends. Nothing is overwritten**, so there is no `If-Match` and no `412`: two people appending from the same base produce two revisions, both in history, the second current. The response says which revision this append produced and which one was current when the request was read — a gap between `followed.seq` and `revision.seq` means somebody else appended in between, which is E8's to render (\"someone else appended while you were typing\") rather than the server's to refuse.  The session's account confirms the text; an agent session answers `403`. A title omitted keeps the current one — a rename is a revision exactly as an edit is. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  ///
  /// * [AppendRevisionRequest] appendRevisionRequest (required):
  Future<Response> appendRevisionWithHttpInfo(String ref, AppendRevisionRequest appendRevisionRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/notes/{ref}/revisions'
      .replaceAll('{ref}', ref);

    // ignore: prefer_final_locals
    Object? postBody = appendRevisionRequest;

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

  /// Append a revision.
  ///
  /// **Always appends. Nothing is overwritten**, so there is no `If-Match` and no `412`: two people appending from the same base produce two revisions, both in history, the second current. The response says which revision this append produced and which one was current when the request was read — a gap between `followed.seq` and `revision.seq` means somebody else appended in between, which is E8's to render (\"someone else appended while you were typing\") rather than the server's to refuse.  The session's account confirms the text; an agent session answers `403`. A title omitted keeps the current one — a rename is a revision exactly as an edit is. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  ///
  /// * [AppendRevisionRequest] appendRevisionRequest (required):
  Future<AppendResult?> appendRevision(String ref, AppendRevisionRequest appendRevisionRequest, { Future<void>? abortTrigger, }) async {
    final response = await appendRevisionWithHttpInfo(ref, appendRevisionRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'AppendResult',) as AppendResult;
    
    }
    return null;
  }

  /// Create a note, as the authenticated person.
  ///
  /// The session's account is the author AND the confirming person. The store refuses a confirmer that is an agent (`note_revisions_guard`, CH041); this operation passes the account through and does not work around the guard, so an agent session answers `403`.  The page is resolved WITHOUT following redirects, for the reason `createPage` gives. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [NewNoteRequest] newNoteRequest (required):
  Future<Response> createNoteWithHttpInfo(NewNoteRequest newNoteRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/notes';

    // ignore: prefer_final_locals
    Object? postBody = newNoteRequest;

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

  /// Create a note, as the authenticated person.
  ///
  /// The session's account is the author AND the confirming person. The store refuses a confirmer that is an agent (`note_revisions_guard`, CH041); this operation passes the account through and does not work around the guard, so an agent session answers `403`.  The page is resolved WITHOUT following redirects, for the reason `createPage` gives. 
  ///
  /// Parameters:
  ///
  /// * [NewNoteRequest] newNoteRequest (required):
  Future<Note?> createNote(NewNoteRequest newNoteRequest, { Future<void>? abortTrigger, }) async {
    final response = await createNoteWithHttpInfo(newNoteRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Note',) as Note;
    
    }
    return null;
  }

  /// A note, rendered, with the references its text names.
  ///
  /// **A pure tier-2 read.** `html` is the current body rendered; `references` are the descriptors the scan produced, and nothing an upstream said about them — batch them to `POST /references/resolve` for cards. No upstream is dialled, which is what lets the `ETag` mean what it says. What links *here* is `GET /notes/{ref}/backlinks`, a sibling rather than a field, for the same reason: it changes when other notes do.  The `ETag` is the current revision id. Send it back as `If-None-Match` and an unchanged note answers `304` with no body.  A soft-deleted note answers `410` with a tombstone: the note existed, it was withdrawn, and by whom and when — and no title and no body. A client holding a link can tell *withdrawn* from *never existed*, which is what CHRN-39's journal is for; undelete makes the same URL answer `200` again. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  ///
  /// * [String] ifNoneMatch:
  ///   The `ETag` of the last read. A match answers `304`.
  Future<Response> getNoteWithHttpInfo(String ref, { String? ifNoneMatch, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/notes/{ref}'
      .replaceAll('{ref}', ref);

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

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

  /// A note, rendered, with the references its text names.
  ///
  /// **A pure tier-2 read.** `html` is the current body rendered; `references` are the descriptors the scan produced, and nothing an upstream said about them — batch them to `POST /references/resolve` for cards. No upstream is dialled, which is what lets the `ETag` mean what it says. What links *here* is `GET /notes/{ref}/backlinks`, a sibling rather than a field, for the same reason: it changes when other notes do.  The `ETag` is the current revision id. Send it back as `If-None-Match` and an unchanged note answers `304` with no body.  A soft-deleted note answers `410` with a tombstone: the note existed, it was withdrawn, and by whom and when — and no title and no body. A client holding a link can tell *withdrawn* from *never existed*, which is what CHRN-39's journal is for; undelete makes the same URL answer `200` again. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  ///
  /// * [String] ifNoneMatch:
  ///   The `ETag` of the last read. A match answers `304`.
  Future<Note?> getNote(String ref, { String? ifNoneMatch, Future<void>? abortTrigger, }) async {
    final response = await getNoteWithHttpInfo(ref, ifNoneMatch: ifNoneMatch, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Note',) as Note;
    
    }
    return null;
  }

  /// The memos this note's text came from, oldest revision first.
  ///
  /// Board `1c`'s `FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE` line and the `▶ PLAY SOURCE AUDIO · 1:44 · PRUNES 2026-09-20` control under the body, from one request and with no date a client had to compute. (The third part of that first line needs nothing from here: `RevisionMeta.verb` is already on the wire and is non-null exactly when a person confirmed a Scribe proposal.)  **A list, because `tier2.note_revisions.memo_id` is per revision.** `0011` chose that shape over a single `notes.memo_id` deliberately and gave the case: a vague memo in March, then a concrete one after a trade show in June, are two memos feeding two revisions of one note. One entry per revision that came from a memo, oldest first — `listNoteRevisions`'s order, so a header renders `items[0]` and a history view can zip the two lists by `revision_seq`. A note somebody typed answers an empty list, not a `404`.  **A sibling of `getNote` rather than a field on it, for a stronger version of `listNoteBacklinks`'s reason.** A note's `ETag` is its current revision id, and `retention_status`, `prunes_at` and `audio_pruned_at` all move while no revision is appended — the pruner sweeps at 03:00 and nothing about the note has changed. Inline, an unchanged note would answer `304` carrying a `PRUNES` date for audio that went hours ago and a play control over nothing. This payload carries no `ETag` and is re-fetched on its own, which is also what makes it skippable: `Note.revision.memo_id` already tells a client whether there is any provenance to ask for.  **It is not a `Memo`.** `content_hash` scoped by its author IS the storage path, and `original_filename` is authored text arriving from a client; neither is here, so a member reading a shared note learns about the recording without being handed the layout of somebody else's. Whether this caller may have the words or the bytes is `transcript.readable` and `audio_readable` — a permission on the payload, so a client renders the play control's absence rather than discovering it through a failed request.  `retention_status` is `store.RetentionStatus` unchanged: the same clause the pruner sweeps with, which is what makes the date rendered here the date the job will use. There is no cursor — N is 1 for every note in the live corpus, and a window no fixture crosses is an intention rather than a contract.  A soft-deleted note answers `410` with the tombstone, the rule every other note sub-resource follows. The two memo operations are unaffected by a note's withdrawal: a memo is its own tier-2 fact and did not stop having been said. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  Future<Response> getNoteProvenanceWithHttpInfo(String ref, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/notes/{ref}/provenance'
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

  /// The memos this note's text came from, oldest revision first.
  ///
  /// Board `1c`'s `FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE` line and the `▶ PLAY SOURCE AUDIO · 1:44 · PRUNES 2026-09-20` control under the body, from one request and with no date a client had to compute. (The third part of that first line needs nothing from here: `RevisionMeta.verb` is already on the wire and is non-null exactly when a person confirmed a Scribe proposal.)  **A list, because `tier2.note_revisions.memo_id` is per revision.** `0011` chose that shape over a single `notes.memo_id` deliberately and gave the case: a vague memo in March, then a concrete one after a trade show in June, are two memos feeding two revisions of one note. One entry per revision that came from a memo, oldest first — `listNoteRevisions`'s order, so a header renders `items[0]` and a history view can zip the two lists by `revision_seq`. A note somebody typed answers an empty list, not a `404`.  **A sibling of `getNote` rather than a field on it, for a stronger version of `listNoteBacklinks`'s reason.** A note's `ETag` is its current revision id, and `retention_status`, `prunes_at` and `audio_pruned_at` all move while no revision is appended — the pruner sweeps at 03:00 and nothing about the note has changed. Inline, an unchanged note would answer `304` carrying a `PRUNES` date for audio that went hours ago and a play control over nothing. This payload carries no `ETag` and is re-fetched on its own, which is also what makes it skippable: `Note.revision.memo_id` already tells a client whether there is any provenance to ask for.  **It is not a `Memo`.** `content_hash` scoped by its author IS the storage path, and `original_filename` is authored text arriving from a client; neither is here, so a member reading a shared note learns about the recording without being handed the layout of somebody else's. Whether this caller may have the words or the bytes is `transcript.readable` and `audio_readable` — a permission on the payload, so a client renders the play control's absence rather than discovering it through a failed request.  `retention_status` is `store.RetentionStatus` unchanged: the same clause the pruner sweeps with, which is what makes the date rendered here the date the job will use. There is no cursor — N is 1 for every note in the live corpus, and a window no fixture crosses is an intention rather than a contract.  A soft-deleted note answers `410` with the tombstone, the rule every other note sub-resource follows. The two memo operations are unaffected by a note's withdrawal: a memo is its own tier-2 fact and did not stop having been said. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  Future<ProvenanceList?> getNoteProvenance(String ref, { Future<void>? abortTrigger, }) async {
    final response = await getNoteProvenanceWithHttpInfo(ref, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'ProvenanceList',) as ProvenanceList;
    
    }
    return null;
  }

  /// The notes whose text names this one, resolved.
  ///
  /// **Derived, and read by tier 2 from its own index.** `tier1.note_links` is extracted from each note's current revision inside the transaction that writes it (CHRN-42), so the graph is never stale with respect to the text it came from; this route resolves those edges against `tier2.notes` to a ref and a title, oldest note first. The payload carries the `Generated` marking with `source: chronicle`, because the list is regenerable and nobody authored it — delete every row and `RebuildNoteLinks` puts them back.  **A sibling of `getNote` rather than a field on it, because of the `ETag`.** A note's `ETag` is its current revision id, and it can mean that only while nothing in the payload changes unless a revision is appended. Backlinks change when *other* notes change. Folding them in would make an unchanged note answer `304` with a stale link list, or force the `ETag` to become a hash of the whole graph; neither is honest, so they live here, uncached.  **Nothing here runs on the tier-1 pool.** The decision recorded on CHRN-100 (2026-09-14) found that `chronicle_tier1` cannot resolve the edges — it may not read `tier2.notes` or `tier2.note_revisions` — so a read through that pool would be raw numbers with resolution left to tier 2. The whole read is tier 2's, over the store the `notes` group already holds; the row is tier 1 and the marking says so.  Paginated by cursor over the source note's number, which is minted once and never reused. A soft-deleted source note drops out of the list; a soft-deleted target answers `410` as `getNote` does. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  ///
  /// * [String] cursor:
  ///   Opaque; the `next_cursor` of the previous page. Absent means the start. Never an offset: every list here is over an append-only table, and an offset silently repeats and skips rows as new ones land. 
  Future<Response> listNoteBacklinksWithHttpInfo(String ref, { int? limit, String? cursor, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/notes/{ref}/backlinks'
      .replaceAll('{ref}', ref);

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

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

  /// The notes whose text names this one, resolved.
  ///
  /// **Derived, and read by tier 2 from its own index.** `tier1.note_links` is extracted from each note's current revision inside the transaction that writes it (CHRN-42), so the graph is never stale with respect to the text it came from; this route resolves those edges against `tier2.notes` to a ref and a title, oldest note first. The payload carries the `Generated` marking with `source: chronicle`, because the list is regenerable and nobody authored it — delete every row and `RebuildNoteLinks` puts them back.  **A sibling of `getNote` rather than a field on it, because of the `ETag`.** A note's `ETag` is its current revision id, and it can mean that only while nothing in the payload changes unless a revision is appended. Backlinks change when *other* notes change. Folding them in would make an unchanged note answer `304` with a stale link list, or force the `ETag` to become a hash of the whole graph; neither is honest, so they live here, uncached.  **Nothing here runs on the tier-1 pool.** The decision recorded on CHRN-100 (2026-09-14) found that `chronicle_tier1` cannot resolve the edges — it may not read `tier2.notes` or `tier2.note_revisions` — so a read through that pool would be raw numbers with resolution left to tier 2. The whole read is tier 2's, over the store the `notes` group already holds; the row is tier 1 and the marking says so.  Paginated by cursor over the source note's number, which is minted once and never reused. A soft-deleted source note drops out of the list; a soft-deleted target answers `410` as `getNote` does. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  ///
  /// * [String] cursor:
  ///   Opaque; the `next_cursor` of the previous page. Absent means the start. Never an offset: every list here is over an append-only table, and an offset silently repeats and skips rows as new ones land. 
  Future<BacklinkList?> listNoteBacklinks(String ref, { int? limit, String? cursor, Future<void>? abortTrigger, }) async {
    final response = await listNoteBacklinksWithHttpInfo(ref, limit: limit, cursor: cursor, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'BacklinkList',) as BacklinkList;
    
    }
    return null;
  }

  /// A note's history, oldest first, with each revision's text.
  ///
  /// Raw markdown per revision, not rendered: history is what was written, and rendering every revision would scan every revision. Paginated by cursor over `seq`, which only ever grows — a restore APPENDS the old text as a new revision rather than moving the pointer back, so this list is a complete account of what was displayed and when. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  ///
  /// * [String] cursor:
  ///   Opaque; the `next_cursor` of the previous page. Absent means the start. Never an offset: every list here is over an append-only table, and an offset silently repeats and skips rows as new ones land. 
  Future<Response> listNoteRevisionsWithHttpInfo(String ref, { int? limit, String? cursor, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/notes/{ref}/revisions'
      .replaceAll('{ref}', ref);

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

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

  /// A note's history, oldest first, with each revision's text.
  ///
  /// Raw markdown per revision, not rendered: history is what was written, and rendering every revision would scan every revision. Paginated by cursor over `seq`, which only ever grows — a restore APPENDS the old text as a new revision rather than moving the pointer back, so this list is a complete account of what was displayed and when. 
  ///
  /// Parameters:
  ///
  /// * [String] ref (required):
  ///   A note reference, parsed leniently — `CHR-0311`, `chr-311` and `CHR-00311` all name note 311 — because people quote these by hand. Rendered strictly everywhere in a payload, as `CHR-0311`. 
  ///
  /// * [int] limit:
  ///   How many to return. CLAMPED SERVER-SIDE, never refused: a triage batch caps at 25 and echoes the cap, `search` caps at 100 and echoes it, and the note and revision lists cap at 200 and say so by answering a `next_cursor` for the rest. A client asking for more than the cap gets the cap. 
  ///
  /// * [String] cursor:
  ///   Opaque; the `next_cursor` of the previous page. Absent means the start. Never an offset: every list here is over an append-only table, and an offset silently repeats and skips rows as new ones land. 
  Future<RevisionList?> listNoteRevisions(String ref, { int? limit, String? cursor, Future<void>? abortTrigger, }) async {
    final response = await listNoteRevisionsWithHttpInfo(ref, limit: limit, cursor: cursor, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'RevisionList',) as RevisionList;
    
    }
    return null;
  }

  /// The live notes filed on a page.
  ///
  /// Soft-deleted notes are excluded, by the store rather than by this handler, so a delete cannot be forgotten by one read surface.  `page` follows a redirect left by a move: a client holding an old path still gets the page, and `moved_from` says the path it asked for is no longer the page's own. Paginated by cursor over the note number, which is append-only. 
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
  Future<Response> listNotesWithHttpInfo(String page, { int? limit, String? cursor, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/notes';

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

  /// The live notes filed on a page.
  ///
  /// Soft-deleted notes are excluded, by the store rather than by this handler, so a delete cannot be forgotten by one read surface.  `page` follows a redirect left by a move: a client holding an old path still gets the page, and `moved_from` says the path it asked for is no longer the page's own. Paginated by cursor over the note number, which is append-only. 
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
  Future<NoteList?> listNotes(String page, { int? limit, String? cursor, Future<void>? abortTrigger, }) async {
    final response = await listNotesWithHttpInfo(page, limit: limit, cursor: cursor, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'NoteList',) as NoteList;
    
    }
    return null;
  }
}
