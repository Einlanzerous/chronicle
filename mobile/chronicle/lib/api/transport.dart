/// The HTTP seam under the generated client: the credential, a deadline, and one
/// refusal.
///
/// The generated `chronicle_api` package takes any `http.Client`, so everything
/// that is true of *every* request lives here rather than at 50 call sites.
library;

import 'dart:async';

import 'package:http/http.dart' as http;

/// Chronicle answered, but not with an answer for this app.
///
/// **Why this exists.** Chronicle serves two hostnames off one backend, and only
/// one of them is the app's: `chronicle-direct.…` has no Access application,
/// while `chronicle.…` sits behind Cloudflare Access. Point a bearer token at
/// the second and Access answers **302 to its own login page** — HTML, for a
/// browser, which no amount of retrying will get past. CHRN-59's own description
/// is why the app must not try: *"browser SSO in front of a native client is a
/// cookie dance that fails at the worst moments."*
///
/// Left to itself `http` **follows** that redirect and hands back a 200 whose
/// body is a Cloudflare login page. The generated client then fails to decode it
/// and reports a parse error — which reads like a broken server rather than like
/// the one thing that is actually wrong, a phone pointed at the browser host. So
/// [ChronicleClient] turns redirect-following off and refuses instead.
///
/// Refusing every redirect is safe rather than merely convenient: `openapi.yaml`
/// declares **no 3xx response on any of its 50 operations** (the two `304`s are
/// cache validators, not redirects), so a redirect is by construction not
/// Chronicle's own answer.
class NotChronicleException implements Exception {
  NotChronicleException({
    required this.statusCode,
    required this.location,
    required this.requestedUrl,
  });

  final int statusCode;
  final String? location;
  final Uri requestedUrl;

  /// True when the redirect target is a Cloudflare Access login, which names the
  /// mistake exactly: this is the tunneled, Access-gated host. Anything else is
  /// still refused, but described as an unexpected redirect rather than
  /// diagnosed — saying "wrong host" about a redirect we cannot attribute would
  /// be a guess dressed as a diagnosis.
  bool get isAccessGated =>
      (location ?? '').contains('cloudflareaccess.com') ||
      (location ?? '').contains('/cdn-cgi/access/');

  @override
  String toString() => isAccessGated
      ? 'NotChronicleException($statusCode): $requestedUrl is behind Cloudflare '
          'Access. Use the direct host.'
      : 'NotChronicleException($statusCode): $requestedUrl redirected to '
          '${location ?? "(no Location)"}';
}

/// Wraps an inner client with the three things every Chronicle request needs.
///
/// 1. **The credential.** `Authorization: Bearer <token>` when one is held. The
///    server reads that header first and falls back to the cookie, so the app
///    never touches a cookie jar.
/// 2. **A deadline.** Dart's [http.Client] never times out on its own, so
///    without this an unreachable server leaves a screen on its spinner
///    forever instead of saying it cannot reach the server. That is the
///    difference between "the tunnel is down and the app says so" and "the app
///    is hung", and the epic is explicit about which one loses the operator's
///    trust.
/// 3. **The refusal above.**
class ChronicleClient extends http.BaseClient {
  ChronicleClient({
    http.Client? inner,
    this.token,
    this.timeout = const Duration(seconds: 12),
  }) : _inner = inner ?? http.Client(),
       _ownsInner = inner == null;

  final http.Client _inner;
  final bool _ownsInner;

  /// The session token, or null when this device is not signed in. Unauthorised
  /// requests are not special-cased: the server answers 401 and the caller
  /// decides what that means, because it depends on who asked. A 401 from
  /// `POST /auth/session` is a spent invite — expected input at the front door.
  /// A 401 from anything else is a session that has ended.
  final String? token;

  final Duration timeout;

  @override
  Future<http.StreamedResponse> send(http.BaseRequest request) async {
    final held = token;
    if (held != null && held.isNotEmpty) {
      request.headers['Authorization'] = 'Bearer $held';
    }

    // Must be set before the request is sent, and is why a redirect reaches us
    // at all rather than being followed into a login page.
    request.followRedirects = false;

    // Bounds the wait for response HEADERS. The body stream is not covered
    // here: a connection that stalls mid-body needs a budget of its own, and
    // the place that will actually need one is CHRN-61's upload queue, which
    // streams audio rather than reading a short JSON document.
    final response = await _inner.send(request).timeout(timeout);

    if (response.isRedirect || (response.statusCode >= 300 && response.statusCode <= 308 && response.statusCode != 304)) {
      // Drain rather than leak the socket: nothing reads this body, and an
      // un-drained stream keeps the connection open for the pool's lifetime.
      unawaited(response.stream.drain<void>().catchError((_) {}));
      throw NotChronicleException(
        statusCode: response.statusCode,
        location: response.headers['location'],
        requestedUrl: request.url,
      );
    }

    return response;
  }

  @override
  void close() {
    if (_ownsInner) _inner.close();
  }
}
