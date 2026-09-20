/// Signing in, signing out, and who we are.
library;

import 'package:chronicle_api/api.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/providers.dart';
import '../api/reachability.dart';
import '../api/server_url.dart';
import '../api/session.dart';
import 'device_label.dart';

/// What went wrong at the front door, in terms the sign-in screen can say out
/// loud. Distinct from [ServerStatus] because these are answers to "can I sign
/// in with this", not "is a server there".
enum SignInFailure {
  /// The scanned or pasted text was not a Chronicle sign-in link.
  notASignInLink,

  /// A Chronicle did not answer on the address the link carries. Carries the
  /// probe's own verdict in [SignInResult.status] so the screen can distinguish
  /// an Access-gated host from a dead one.
  serverUnreachable,

  /// **401.** Spent, expired, or never existed — the server answers all three
  /// identically and deliberately, so that probing cannot tell a used invite
  /// from one that was never issued. The copy must not guess which it was.
  inviteRejected,

  /// The sign-in endpoint's own rate limit (429). Waiting fixes it.
  rateLimited,

  /// Anything else the server said.
  serverError,
}

class SignInResult {
  const SignInResult.ok(this.user)
      : failure = null,
        status = null,
        statusCode = null;

  const SignInResult.failed(this.failure, {this.status, this.statusCode})
      : user = null;

  final User? user;
  final SignInFailure? failure;

  /// Set when [failure] is [SignInFailure.serverUnreachable].
  final ServerStatus? status;

  /// Set when the server answered and the answer was not 200.
  final int? statusCode;

  bool get ok => failure == null;

  String get message => switch (failure) {
        null => 'Signed in',
        SignInFailure.notASignInLink =>
          'That is not a Chronicle sign-in code. Scan the code from Account → '
              'Add device.',
        SignInFailure.serverUnreachable =>
          status?.message ?? 'Cannot reach that server',
        SignInFailure.inviteRejected =>
          'That sign-in code has been used or has expired. Generate a new one '
              'from Account → Add device.',
        SignInFailure.rateLimited =>
          'Too many sign-in attempts. Wait a moment and try again.',
        SignInFailure.serverError =>
          'The server refused the sign-in${statusCode == null ? '' : ' ($statusCode)'}',
      };
}

class AuthController extends Notifier<void> {
  @override
  void build() {}

  /// Redeem an invite for a durable session on this device.
  ///
  /// **The address is stored before the redemption is attempted**, because the
  /// generated client is built from it — there is no way to redeem against an
  /// address the client does not hold. The order is safe in the direction that
  /// matters: storing an address costs nothing and is corrected by the next
  /// scan, while a session token is only ever written after a 200.
  ///
  /// The address is **probed first**. A `POST` to a host that turns out to be
  /// Access-gated would spend the single-use invite against a login page, and
  /// the invite is single-use: the operator would then need a fresh one for a
  /// mistake that costs one unauthenticated GET to rule out.
  Future<SignInResult> signIn({
    required String baseUrl,
    required String inviteToken,
    String? label,
  }) async {
    await ref.read(serverUrlProvider.notifier).set(baseUrl);

    final status = await ref.read(reachabilityProvider.notifier).check();
    if (!status.isOk) {
      return SignInResult.failed(
        SignInFailure.serverUnreachable,
        status: status,
      );
    }

    final api = ref.read(authApiProvider);
    try {
      final session = await api.createSession(
        SignInRequest(
          // Whitespace is stripped rather than trimmed: a token pasted out of a
          // chat app or a terminal arrives hard-wrapped, and a key broken across
          // two lines is still the key someone was given.
          token: inviteToken.replaceAll(RegExp(r'\s+'), ''),
          deviceLabel: label ?? await deviceLabel(),
        ),
      );
      final token = session?.sessionToken ?? '';
      if (token.isEmpty) {
        // A 200 with no token is not a session. Refusing here beats storing an
        // empty string that every later request presents as a credential.
        return const SignInResult.failed(SignInFailure.serverError);
      }
      await ref.read(sessionTokenProvider.notifier).set(token);
      return SignInResult.ok(session!.user);
    } on ApiException catch (e) {
      return SignInResult.failed(
        switch (e.code) {
          401 => SignInFailure.inviteRejected,
          429 => SignInFailure.rateLimited,
          _ => SignInFailure.serverError,
        },
        statusCode: e.code,
      );
    } catch (_) {
      return const SignInResult.failed(SignInFailure.serverUnreachable);
    }
  }

  /// Sign out this device, revoking the session server-side.
  ///
  /// Returns true when the server confirmed the revocation. **The local token is
  /// cleared either way, and that is a deliberate asymmetry**: refusing to sign
  /// out while offline would strand a person holding a device they want to hand
  /// over, and a session they cannot revoke locally is not made safer by the app
  /// continuing to use it. When the server was not reached the caller says so,
  /// so the session can be revoked from the device list later.
  Future<bool> signOut() async {
    var revoked = false;
    try {
      await ref.read(authApiProvider).deleteSession();
      revoked = true;
    } catch (_) {
      revoked = false;
    }
    await ref.read(sessionTokenProvider.notifier).clear();
    return revoked;
  }
}

final authControllerProvider =
    NotifierProvider<AuthController, void>(AuthController.new);

/// The signed-in account. Null when the session has ended — `GET /auth/me`
/// answering 401 is how a client learns its token was revoked from another
/// device, which is the only way it can learn.
final meProvider = FutureProvider<User?>((ref) async {
  if (!ref.watch(isSignedInProvider)) return null;
  try {
    return await ref.watch(authApiProvider).getMe();
  } on ApiException catch (e) {
    if (e.code == 401) {
      await ref.read(sessionTokenProvider.notifier).clear();
      return null;
    }
    rethrow;
  }
});
