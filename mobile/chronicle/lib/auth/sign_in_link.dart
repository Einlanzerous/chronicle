/// Reading the QR code CHRN-106 renders.
///
/// The server builds `<base>/sign-in?token=…` in `internal/invite/url.go`, and
/// that one string carries **both** facts a new device needs: the address and the
/// invite. One scan sets both, so nobody types a URL — which is the whole reason
/// the link exists rather than the app asking for a hostname and then a token.
///
/// The base is taken from the link and never inferred. `invite.SignInURL`'s own
/// comment says why: *"Only the server knows which of its origins a phone can
/// reach."*
library;

/// A scanned sign-in link, split into the two things it carries.
class SignInLink {
  const SignInLink({required this.baseUrl, required this.token});

  /// The origin to talk to, normalised the way `invite.NormalizeBase` does:
  /// scheme + authority, no path, no trailing slash.
  final String baseUrl;

  /// The single-use invite token.
  final String token;

  @override
  String toString() => 'SignInLink($baseUrl)';
}

/// Parses a scanned payload, or returns null when it is not a Chronicle sign-in
/// link.
///
/// Accepts what a scanner and a person actually produce:
///
///  * the exact link the server mints, `https://host/sign-in?token=abc`;
///  * the same link with a port, a subpath (`https://host/chronicle/sign-in`),
///    or extra query parameters;
///  * surrounding whitespace, which QR readers and chat apps both add.
///
/// Rejects anything without the `/sign-in` path and a non-empty `token`, because
/// the failure to be avoided is confidently storing a base URL read off an
/// unrelated QR code — a Wi-Fi credential, a parcel tracking link — and then
/// reporting "cannot reach the server" about an address that was never a server.
SignInLink? parseSignInLink(String raw) {
  final trimmed = raw.trim();
  if (trimmed.isEmpty) return null;

  final Uri uri;
  try {
    uri = Uri.parse(trimmed);
  } on FormatException {
    return null;
  }

  if (uri.scheme != 'http' && uri.scheme != 'https') return null;
  if (!uri.hasAuthority || uri.host.isEmpty) return null;

  // The path must END with /sign-in: a deployment served under a subpath mints
  // `https://host/chronicle/sign-in?token=…`, and the base is everything before
  // that segment.
  final segments = uri.pathSegments.where((s) => s.isNotEmpty).toList();
  if (segments.isEmpty || segments.last != 'sign-in') return null;

  final token = uri.queryParameters['token']?.trim() ?? '';
  if (token.isEmpty) return null;

  final basePath = segments.sublist(0, segments.length - 1).join('/');
  final base = StringBuffer('${uri.scheme}://${uri.authority}');
  if (basePath.isNotEmpty) base.write('/$basePath');

  return SignInLink(baseUrl: base.toString(), token: token);
}
