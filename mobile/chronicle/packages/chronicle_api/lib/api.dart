//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

library openapi.api;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:collection/collection.dart';
import 'package:http/http.dart';
import 'package:intl/intl.dart';
import 'package:meta/meta.dart';

part 'api_client.dart';
part 'api_helper.dart';
part 'api_exception.dart';
part 'auth/authentication.dart';
part 'auth/api_key_auth.dart';
part 'auth/oauth.dart';
part 'auth/http_basic_auth.dart';
part 'auth/http_bearer_auth.dart';

part 'api/accounts_api.dart';
part 'api/auth_api.dart';
part 'api/discussions_api.dart';
part 'api/memos_api.dart';
part 'api/meta_api.dart';
part 'api/notes_api.dart';
part 'api/pages_api.dart';
part 'api/references_api.dart';
part 'api/search_api.dart';
part 'api/storage_api.dart';
part 'api/tier1_api.dart';
part 'api/transcription_api.dart';
part 'api/triage_api.dart';
part 'api/uploads_api.dart';

part 'model/accept_request.dart';
part 'model/append_result.dart';
part 'model/append_revision_request.dart';
part 'model/backlink.dart';
part 'model/backlink_list.dart';
part 'model/backlog_report.dart';
part 'model/batch_item.dart';
part 'model/cleared_field.dart';
part 'model/corpus_report.dart';
part 'model/deferred_item.dart';
part 'model/deferred_list.dart';
part 'model/device_session.dart';
part 'model/discussion.dart';
part 'model/discussion_list.dart';
part 'model/discussion_resolution.dart';
part 'model/discussion_summary.dart';
part 'model/disk_report.dart';
part 'model/error.dart';
part 'model/generated.dart';
part 'model/health.dart';
part 'model/held_memo.dart';
part 'model/hold_request.dart';
part 'model/invite.dart';
part 'model/link_state.dart';
part 'model/mark_read_request.dart';
part 'model/member.dart';
part 'model/memo.dart';
part 'model/memo_provenance.dart';
part 'model/memo_transcript.dart';
part 'model/mismatch.dart';
part 'model/new_discussion_request.dart';
part 'model/new_note_request.dart';
part 'model/new_page_request.dart';
part 'model/new_turn_request.dart';
part 'model/new_user_request.dart';
part 'model/note.dart';
part 'model/note_list.dart';
part 'model/note_summary.dart';
part 'model/note_tombstone.dart';
part 'model/open_upload409_response.dart';
part 'model/open_upload_request.dart';
part 'model/override.dart';
part 'model/page.dart';
part 'model/page_tree.dart';
part 'model/partial_transcript.dart';
part 'model/participant.dart';
part 'model/participant_request.dart';
part 'model/proposal.dart';
part 'model/provenance_list.dart';
part 'model/provenance_transcript.dart';
part 'model/readiness.dart';
part 'model/reconciliation_report.dart';
part 'model/reference_descriptor.dart';
part 'model/release_request.dart';
part 'model/resolution.dart';
part 'model/resolution_upstream.dart';
part 'model/resolve_discussion_request.dart';
part 'model/resolve_request.dart';
part 'model/resolve_response.dart';
part 'model/resolved_note.dart';
part 'model/resolved_state.dart';
part 'model/revision.dart';
part 'model/revision_list.dart';
part 'model/revision_meta.dart';
part 'model/revision_pointer.dart';
part 'model/search_hit.dart';
part 'model/search_results.dart';
part 'model/session.dart';
part 'model/sign_in_request.dart';
part 'model/sso_error.dart';
part 'model/storage_report.dart';
part 'model/thread.dart';
part 'model/tier1_page.dart';
part 'model/tier1_page_list.dart';
part 'model/tier1_page_summary.dart';
part 'model/transcript_segment.dart';
part 'model/transcription_report.dart';
part 'model/triage_batch.dart';
part 'model/triage_decision.dart';
part 'model/triage_report.dart';
part 'model/triage_result.dart';
part 'model/triage_results.dart';
part 'model/turn.dart';
part 'model/unread_item.dart';
part 'model/unread_list.dart';
part 'model/update_me_request.dart';
part 'model/upload_state.dart';
part 'model/user.dart';
part 'model/window_report.dart';


/// An [ApiClient] instance that uses the default values obtained from
/// the OpenAPI specification file.
var defaultApiClient = ApiClient();

const _delimiters = {'csv': ',', 'ssv': ' ', 'tsv': '\t', 'pipes': '|'};
const _dateEpochMarker = 'epoch';
const _deepEquality = DeepCollectionEquality();
final _dateFormatter = DateFormat('yyyy-MM-dd');
final _regList = RegExp(r'^List<(.*)>$');
final _regSet = RegExp(r'^Set<(.*)>$');
final _regMap = RegExp(r'^Map<String,(.*)>$');

bool _isEpochMarker(String? pattern) => pattern == _dateEpochMarker || pattern == '/$_dateEpochMarker/';
