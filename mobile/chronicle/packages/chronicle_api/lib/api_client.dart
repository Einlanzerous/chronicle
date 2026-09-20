//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ApiClient {
  ApiClient({this.basePath = 'http://chronicle:4009', this.authentication,});

  final String basePath;
  final Authentication? authentication;

  var _client = Client();
  final _defaultHeaderMap = <String, String>{};

  /// Returns the current HTTP [Client] instance to use in this class.
  ///
  /// The return value is guaranteed to never be null.
  Client get client => _client;

  /// Requests to use a new HTTP [Client] in this class.
  set client(Client newClient) {
    _client = newClient;
  }

  Map<String, String> get defaultHeaderMap => _defaultHeaderMap;

  void addDefaultHeader(String key, String value) {
     _defaultHeaderMap[key] = value;
  }

  // We don't use a Map<String, String> for queryParams.
  // If collectionFormat is 'multi', a key might appear multiple times.
  Future<Response> invokeAPI(
    String path,
    String method,
    List<QueryParam> queryParams,
    Object? body,
    Map<String, String> headerParams,
    Map<String, String> formParams,
    String? contentType, {
    Future<void>? abortTrigger,
  }) async {
    await authentication?.applyToParams(queryParams, headerParams);

    headerParams.addAll(_defaultHeaderMap);
    if (contentType != null) {
      headerParams['Content-Type'] = contentType;
    }

    final urlEncodedQueryParams = queryParams.map((param) => '$param');
    final queryString = urlEncodedQueryParams.isNotEmpty ? '?${urlEncodedQueryParams.join('&')}' : '';
    final uri = Uri.parse('$basePath$path$queryString');

    try {
      // Special case for uploading a single file which isn't a 'multipart/form-data'.
      if (
        body is MultipartFile && (contentType == null ||
        !contentType.toLowerCase().startsWith('multipart/form-data'))
      ) {
        final request = AbortableStreamedRequest(method, uri, abortTrigger: abortTrigger);
        request.headers.addAll(headerParams);
        request.contentLength = body.length;
        body.finalize().listen(
          request.sink.add,
          onDone: request.sink.close,
          // ignore: avoid_types_on_closure_parameters
          onError: (Object error, StackTrace trace) => request.sink.close(),
          cancelOnError: true,
        );
        final response = await _client.send(request);
        return Response.fromStream(response);
      }

      if (body is MultipartRequest) {
        final request = AbortableMultipartRequest(method, uri, abortTrigger: abortTrigger);
        request.fields.addAll(body.fields);
        request.files.addAll(body.files);
        request.headers.addAll(body.headers);
        request.headers.addAll(headerParams);
        final response = await _client.send(request);
        return Response.fromStream(response);
      }

      final msgBody = contentType == 'application/x-www-form-urlencoded'
        ? formParams
        : await serializeAsync(body);
      final nullableHeaderParams = headerParams.isEmpty ? null : headerParams;

      final request = AbortableRequest(method, uri, abortTrigger: abortTrigger);
      if (nullableHeaderParams != null) {
        request.headers.addAll(nullableHeaderParams);
      }
      if (msgBody is String && msgBody.isNotEmpty) {
        request.body = msgBody;
      } else if (msgBody is List<int> && msgBody.isNotEmpty) {
        request.bodyBytes = msgBody;
      } else if (msgBody is Map<String, String>) {
        request.bodyFields = msgBody;
      }
      final response = await _client.send(request);
      return Response.fromStream(response);
    } on SocketException catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'Socket operation failed: $method $path',
        error,
        trace,
      );
    } on TlsException catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'TLS/SSL communication failed: $method $path',
        error,
        trace,
      );
    } on IOException catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'I/O operation failed: $method $path',
        error,
        trace,
      );
    } on ClientException catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'HTTP connection failed: $method $path',
        error,
        trace,
      );
    } on Exception catch (error, trace) {
      throw ApiException.withInner(
        HttpStatus.badRequest,
        'Exception occurred: $method $path',
        error,
        trace,
      );
    }
  }

  Future<dynamic> deserializeAsync(String value, String targetType, {bool growable = false,}) async =>
    // ignore: deprecated_member_use_from_same_package
    deserialize(value, targetType, growable: growable);

  @Deprecated('Scheduled for removal in OpenAPI Generator 6.x. Use deserializeAsync() instead.')
  dynamic deserialize(String value, String targetType, {bool growable = false,}) {
    // Remove all spaces. Necessary for regular expressions as well.
    targetType = targetType.replaceAll(' ', ''); // ignore: parameter_assignments

    // If the expected target type is String, nothing to do...
    return targetType == 'String'
      ? value
      : fromJson(json.decode(value), targetType, growable: growable);
  }

  // ignore: deprecated_member_use_from_same_package
  Future<String> serializeAsync(Object? value) async => serialize(value);

  @Deprecated('Scheduled for removal in OpenAPI Generator 6.x. Use serializeAsync() instead.')
  String serialize(Object? value) => value == null ? '' : json.encode(value);

  /// Returns a native instance of an OpenAPI class matching the [specified type][targetType].
  static dynamic fromJson(dynamic value, String targetType, {bool growable = false,}) {
    try {
      switch (targetType) {
        case 'String':
          return value is String ? value : value.toString();
        case 'int':
          return value is int ? value : int.parse('$value');
        case 'double':
          return value is double ? value : double.parse('$value');
        case 'bool':
          if (value is bool) {
            return value;
          }
          final valueString = '$value'.toLowerCase();
          return valueString == 'true' || valueString == '1';
        case 'DateTime':
          return value is DateTime ? value : DateTime.tryParse(value);
        case 'AcceptRequest':
          return AcceptRequest.fromJson(value);
        case 'AppendResult':
          return AppendResult.fromJson(value);
        case 'AppendRevisionRequest':
          return AppendRevisionRequest.fromJson(value);
        case 'Backlink':
          return Backlink.fromJson(value);
        case 'BacklinkList':
          return BacklinkList.fromJson(value);
        case 'BacklogReport':
          return BacklogReport.fromJson(value);
        case 'BatchItem':
          return BatchItem.fromJson(value);
        case 'ClearedField':
          return ClearedField.fromJson(value);
        case 'CorpusReport':
          return CorpusReport.fromJson(value);
        case 'DeferredItem':
          return DeferredItem.fromJson(value);
        case 'DeferredList':
          return DeferredList.fromJson(value);
        case 'DeviceSession':
          return DeviceSession.fromJson(value);
        case 'Discussion':
          return Discussion.fromJson(value);
        case 'DiscussionList':
          return DiscussionList.fromJson(value);
        case 'DiscussionResolution':
          return DiscussionResolution.fromJson(value);
        case 'DiscussionSummary':
          return DiscussionSummary.fromJson(value);
        case 'DiskReport':
          return DiskReport.fromJson(value);
        case 'Error':
          return Error.fromJson(value);
        case 'Generated':
          return Generated.fromJson(value);
        case 'Health':
          return Health.fromJson(value);
        case 'HeldMemo':
          return HeldMemo.fromJson(value);
        case 'HoldRequest':
          return HoldRequest.fromJson(value);
        case 'Invite':
          return Invite.fromJson(value);
        case 'LinkState':
          return LinkState.fromJson(value);
        case 'MarkReadRequest':
          return MarkReadRequest.fromJson(value);
        case 'Member':
          return Member.fromJson(value);
        case 'Memo':
          return Memo.fromJson(value);
        case 'MemoProvenance':
          return MemoProvenance.fromJson(value);
        case 'MemoTranscript':
          return MemoTranscript.fromJson(value);
        case 'Mismatch':
          return Mismatch.fromJson(value);
        case 'NewDiscussionRequest':
          return NewDiscussionRequest.fromJson(value);
        case 'NewNoteRequest':
          return NewNoteRequest.fromJson(value);
        case 'NewPageRequest':
          return NewPageRequest.fromJson(value);
        case 'NewTurnRequest':
          return NewTurnRequest.fromJson(value);
        case 'NewUserRequest':
          return NewUserRequest.fromJson(value);
        case 'Note':
          return Note.fromJson(value);
        case 'NoteList':
          return NoteList.fromJson(value);
        case 'NoteSummary':
          return NoteSummary.fromJson(value);
        case 'NoteTombstone':
          return NoteTombstone.fromJson(value);
        case 'OpenUpload409Response':
          return OpenUpload409Response.fromJson(value);
        case 'OpenUploadRequest':
          return OpenUploadRequest.fromJson(value);
        case 'Override':
          return Override.fromJson(value);
        case 'Page':
          return Page.fromJson(value);
        case 'PageTree':
          return PageTree.fromJson(value);
        case 'PartialTranscript':
          return PartialTranscript.fromJson(value);
        case 'Participant':
          return Participant.fromJson(value);
        case 'ParticipantRequest':
          return ParticipantRequest.fromJson(value);
        case 'Proposal':
          return Proposal.fromJson(value);
        case 'ProvenanceList':
          return ProvenanceList.fromJson(value);
        case 'ProvenanceTranscript':
          return ProvenanceTranscript.fromJson(value);
        case 'Readiness':
          return Readiness.fromJson(value);
        case 'ReconciliationReport':
          return ReconciliationReport.fromJson(value);
        case 'ReferenceDescriptor':
          return ReferenceDescriptor.fromJson(value);
        case 'ReleaseRequest':
          return ReleaseRequest.fromJson(value);
        case 'Resolution':
          return Resolution.fromJson(value);
        case 'ResolutionUpstream':
          return ResolutionUpstream.fromJson(value);
        case 'ResolveDiscussionRequest':
          return ResolveDiscussionRequest.fromJson(value);
        case 'ResolveRequest':
          return ResolveRequest.fromJson(value);
        case 'ResolveResponse':
          return ResolveResponse.fromJson(value);
        case 'ResolvedNote':
          return ResolvedNote.fromJson(value);
        case 'ResolvedState':
          return ResolvedState.fromJson(value);
        case 'Revision':
          return Revision.fromJson(value);
        case 'RevisionList':
          return RevisionList.fromJson(value);
        case 'RevisionMeta':
          return RevisionMeta.fromJson(value);
        case 'RevisionPointer':
          return RevisionPointer.fromJson(value);
        case 'SearchHit':
          return SearchHit.fromJson(value);
        case 'SearchResults':
          return SearchResults.fromJson(value);
        case 'Session':
          return Session.fromJson(value);
        case 'SignInRequest':
          return SignInRequest.fromJson(value);
        case 'SsoError':
          return SsoError.fromJson(value);
        case 'StorageReport':
          return StorageReport.fromJson(value);
        case 'Thread':
          return Thread.fromJson(value);
        case 'Tier1Page':
          return Tier1Page.fromJson(value);
        case 'Tier1PageList':
          return Tier1PageList.fromJson(value);
        case 'Tier1PageSummary':
          return Tier1PageSummary.fromJson(value);
        case 'TranscriptSegment':
          return TranscriptSegment.fromJson(value);
        case 'TranscriptionReport':
          return TranscriptionReport.fromJson(value);
        case 'TriageBatch':
          return TriageBatch.fromJson(value);
        case 'TriageDecision':
          return TriageDecision.fromJson(value);
        case 'TriageReport':
          return TriageReport.fromJson(value);
        case 'TriageResult':
          return TriageResult.fromJson(value);
        case 'TriageResults':
          return TriageResults.fromJson(value);
        case 'Turn':
          return Turn.fromJson(value);
        case 'UnreadItem':
          return UnreadItem.fromJson(value);
        case 'UnreadList':
          return UnreadList.fromJson(value);
        case 'UpdateMeRequest':
          return UpdateMeRequest.fromJson(value);
        case 'UploadState':
          return UploadState.fromJson(value);
        case 'User':
          return User.fromJson(value);
        case 'WindowReport':
          return WindowReport.fromJson(value);
        default:
          dynamic match;
          if (value is List && (match = _regList.firstMatch(targetType)?.group(1)) != null) {
            return value
              .map<dynamic>((dynamic v) => fromJson(v, match, growable: growable,))
              .toList(growable: growable);
          }
          if (value is Set && (match = _regSet.firstMatch(targetType)?.group(1)) != null) {
            return value
              .map<dynamic>((dynamic v) => fromJson(v, match, growable: growable,))
              .toSet();
          }
          if (value is Map && (match = _regMap.firstMatch(targetType)?.group(1)) != null) {
            return Map<String, dynamic>.fromIterables(
              value.keys.cast<String>(),
              value.values.map<dynamic>((dynamic v) => fromJson(v, match, growable: growable,)),
            );
          }
      }
    } on Exception catch (error, trace) {
      throw ApiException.withInner(HttpStatus.internalServerError, 'Exception during deserialization.', error, trace,);
    }
    throw ApiException(HttpStatus.internalServerError, 'Could not find a suitable class for deserialization',);
  }
}

/// Primarily intended for use in an isolate.
class DeserializationMessage {
  const DeserializationMessage({
    required this.json,
    required this.targetType,
    this.growable = false,
  });

  /// The JSON value to deserialize.
  final String json;

  /// Target type to deserialize to.
  final String targetType;

  /// Whether to make deserialized lists or maps growable.
  final bool growable;
}

/// Primarily intended for use in an isolate.
Future<dynamic> decodeAsync(DeserializationMessage message) async {
  // Remove all spaces. Necessary for regular expressions as well.
  final targetType = message.targetType.replaceAll(' ', '');

  // If the expected target type is String, nothing to do...
  return targetType == 'String'
    ? message.json
    : json.decode(message.json);
}

/// Primarily intended for use in an isolate.
Future<dynamic> deserializeAsync(DeserializationMessage message) async {
  // Remove all spaces. Necessary for regular expressions as well.
  final targetType = message.targetType.replaceAll(' ', '');

  // If the expected target type is String, nothing to do...
  return targetType == 'String'
    ? message.json
    : ApiClient.fromJson(
        json.decode(message.json),
        targetType,
        growable: message.growable,
      );
}

/// Primarily intended for use in an isolate.
Future<String> serializeAsync(Object? value) async => value == null ? '' : json.encode(value);
