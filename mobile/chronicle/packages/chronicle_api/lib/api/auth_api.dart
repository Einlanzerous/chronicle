//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class AuthApi {
  AuthApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// Mint an invite for another of your own devices.
  ///
  /// Self-service, so adding a second phone does not need the owner. The invite is single-use and short-lived; `sign_in_url` is the QR target and is absent when no mobile base URL is configured. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> createSelfInviteWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/auth/invite';

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    const contentTypes = <String>[];


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

  /// Mint an invite for another of your own devices.
  ///
  /// Self-service, so adding a second phone does not need the owner. The invite is single-use and short-lived; `sign_in_url` is the QR target and is absent when no mobile base URL is configured. 
  Future<Invite?> createSelfInvite({ Future<void>? abortTrigger, }) async {
    final response = await createSelfInviteWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Invite',) as Invite;
    
    }
    return null;
  }

  /// Redeem an invite for a session.
  ///
  /// How the app and the MCP sign in. The session token is returned here and never again; the client stores it and sends it as `Authorization: Bearer`, or accepts the cookie.  A spent, expired or unknown invite is `401` with an identical body, indistinguishable on purpose — probing cannot tell a used invite from one that never existed. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [SignInRequest] signInRequest (required):
  Future<Response> createSessionWithHttpInfo(SignInRequest signInRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/auth/session';

    // ignore: prefer_final_locals
    Object? postBody = signInRequest;

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

  /// Redeem an invite for a session.
  ///
  /// How the app and the MCP sign in. The session token is returned here and never again; the client stores it and sends it as `Authorization: Bearer`, or accepts the cookie.  A spent, expired or unknown invite is `401` with an identical body, indistinguishable on purpose — probing cannot tell a used invite from one that never existed. 
  ///
  /// Parameters:
  ///
  /// * [SignInRequest] signInRequest (required):
  Future<Session?> createSession(SignInRequest signInRequest, { Future<void>? abortTrigger, }) async {
    final response = await createSessionWithHttpInfo(signInRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Session',) as Session;
    
    }
    return null;
  }

  /// Exchange a verified Cloudflare Access identity for a session.
  ///
  /// The browser calls this on load: the tunnel injects a `Cf-Access-Jwt-Assertion` header, Chronicle verifies it **itself** against the JWKS rather than trusting the header the edge injected, and a verified email matched to an account mints the same kind of session the invite path does.  **The assertion header is deliberately not declared as a parameter.** Declaring it required would make the generated wrapper answer `400` when it is absent, where this operation answers `401` with a code saying which of \"no assertion\", \"not configured here\" and \"no such account\" happened — a distinction a person debugging an SSO wall needs. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> createSessionFromAccessWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/auth/sso/cloudflare';

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    const contentTypes = <String>[];


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

  /// Exchange a verified Cloudflare Access identity for a session.
  ///
  /// The browser calls this on load: the tunnel injects a `Cf-Access-Jwt-Assertion` header, Chronicle verifies it **itself** against the JWKS rather than trusting the header the edge injected, and a verified email matched to an account mints the same kind of session the invite path does.  **The assertion header is deliberately not declared as a parameter.** Declaring it required would make the generated wrapper answer `400` when it is absent, where this operation answers `401` with a code saying which of \"no assertion\", \"not configured here\" and \"no such account\" happened — a distinction a person debugging an SSO wall needs. 
  Future<Session?> createSessionFromAccess({ Future<void>? abortTrigger, }) async {
    final response = await createSessionFromAccessWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'Session',) as Session;
    
    }
    return null;
  }

  /// Sign out, revoking this session.
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> deleteSessionWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/auth/session';

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

  /// Sign out, revoking this session.
  Future<void> deleteSession({ Future<void>? abortTrigger, }) async {
    final response = await deleteSessionWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }

  /// The account this session belongs to.
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> getMeWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/auth/me';

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

  /// The account this session belongs to.
  Future<User?> getMe({ Future<void>? abortTrigger, }) async {
    final response = await getMeWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'User',) as User;
    
    }
    return null;
  }

  /// Your own signed-in devices.
  ///
  /// The one real risk in a password-free model: a session does not expire, so a lost device stays signed in forever unless its holder can see it and cut it off. Hence this list, and the revoke below it. 
  ///
  /// Note: This method returns the HTTP [Response].
  Future<Response> listSessionsWithHttpInfo({ Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/auth/sessions';

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

  /// Your own signed-in devices.
  ///
  /// The one real risk in a password-free model: a session does not expire, so a lost device stays signed in forever unless its holder can see it and cut it off. Hence this list, and the revoke below it. 
  Future<List<DeviceSession>?> listSessions({ Future<void>? abortTrigger, }) async {
    final response = await listSessionsWithHttpInfo(abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      final responseBody = await _decodeBodyBytes(response);
      return (await apiClient.deserializeAsync(responseBody, 'List<DeviceSession>') as List)
        .cast<DeviceSession>()
        .toList(growable: false);

    }
    return null;
  }

  /// Revoke one of your own sessions.
  ///
  /// Revoking the session you are calling with is a sign-out, and clears the cookie too. Another account's session answers exactly as one that does not exist. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] id (required):
  ///   A session's id, from GET /auth/sessions.
  Future<Response> revokeSessionWithHttpInfo(String id, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/auth/sessions/{id}'
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

  /// Revoke one of your own sessions.
  ///
  /// Revoking the session you are calling with is a sign-out, and clears the cookie too. Another account's session answers exactly as one that does not exist. 
  ///
  /// Parameters:
  ///
  /// * [String] id (required):
  ///   A session's id, from GET /auth/sessions.
  Future<void> revokeSession(String id, { Future<void>? abortTrigger, }) async {
    final response = await revokeSessionWithHttpInfo(id, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }

  /// Change your own display name.
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [UpdateMeRequest] updateMeRequest (required):
  Future<Response> updateMeWithHttpInfo(UpdateMeRequest updateMeRequest, { Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/auth/me';

    // ignore: prefer_final_locals
    Object? postBody = updateMeRequest;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    const contentTypes = <String>['application/json'];


    return apiClient.invokeAPI(
      path,
      'PATCH',
      queryParams,
      postBody,
      headerParams,
      formParams,
      contentTypes.isEmpty ? null : contentTypes.first,
      abortTrigger: abortTrigger,
    );
  }

  /// Change your own display name.
  ///
  /// Parameters:
  ///
  /// * [UpdateMeRequest] updateMeRequest (required):
  Future<User?> updateMe(UpdateMeRequest updateMeRequest, { Future<void>? abortTrigger, }) async {
    final response = await updateMeWithHttpInfo(updateMeRequest, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
    // When a remote server returns no body with a status of 204, we shall not decode it.
    // At the time of writing this, `dart:convert` will throw an "Unexpected end of input"
    // FormatException when trying to decode an empty string.
    if (response.body.isNotEmpty && response.statusCode != HttpStatus.noContent) {
      return await apiClient.deserializeAsync(await _decodeBodyBytes(response), 'User',) as User;
    
    }
    return null;
  }
}
