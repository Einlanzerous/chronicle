/// Routing, and the one rule it enforces: a device with no address or no
/// credential sees the front door — **except that it can still record**.
///
/// CHRN-59 shipped the simpler rule, where every route redirected to `/sign-in`
/// without both an address and a token. CHRN-60 has to carve capture out of it,
/// because that rule quietly contradicts the epic's own invariant that *capture
/// must never depend on the network*. With it in place:
///
/// * a device whose session was revoked from *Account → devices* cannot start a
///   recording at all, and
/// * once CHRN-61 lands, a 401 from a background upload clears the token and
///   bounces the UI off the RECORDING screen **mid-memo**.
///
/// Airplane mode exercises neither, because the redirect keys on a stored token
/// rather than on reachability — which is exactly why this was invisible.
///
/// So **sign-in gates sending, never capturing.** A signed-out device records
/// and queues; the queue is what says `SIGN IN TO SEND`. The recording UI is
/// driven by the capture service's own state, so a redirect cannot end a
/// recording that is already running either.
library;

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../api/server_url.dart';
import '../api/session.dart';
import '../features/capture/capture_screen.dart';
import '../features/home/home_screen.dart';
import '../features/signin/sign_in_screen.dart';

/// The one route that is reachable without a credential.
const captureRoute = '/capture';

final routerProvider = Provider<GoRouter>((ref) {
  return GoRouter(
    initialLocation: '/',
    // Rebuilt when either fact changes, so clearing the token on a 401 bounces
    // the app to the front door without any screen having to navigate.
    refreshListenable: _Refresh(ref),
    redirect: (context, state) {
      final ready = ref.read(hasServerProvider) && ref.read(isSignedInProvider);
      final atSignIn = state.matchedLocation == '/sign-in';
      // Capture is outside the gate, in both directions: a signed-out device
      // may reach it, and a signed-in one is never bounced off it.
      if (state.matchedLocation == captureRoute) return null;
      if (!ready) return atSignIn ? null : '/sign-in';
      if (atSignIn) return '/';
      return null;
    },
    routes: [
      GoRoute(path: '/', builder: (_, _) => const HomeScreen()),
      GoRoute(path: '/sign-in', builder: (_, _) => const SignInScreen()),
      GoRoute(path: captureRoute, builder: (_, _) => const CaptureScreen()),
    ],
  );
});

/// Bridges Riverpod's two providers to go_router's [Listenable].
class _Refresh extends ChangeNotifier {
  _Refresh(Ref ref) {
    ref.listen(hasServerProvider, (_, _) => notifyListeners());
    ref.listen(isSignedInProvider, (_, _) => notifyListeners());
  }
}
