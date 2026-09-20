/// Routing, and the one rule it enforces: a device with no address or no
/// credential sees the front door and nothing else.
library;

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../api/server_url.dart';
import '../api/session.dart';
import '../features/home/home_screen.dart';
import '../features/signin/sign_in_screen.dart';

final routerProvider = Provider<GoRouter>((ref) {
  return GoRouter(
    initialLocation: '/',
    // Rebuilt when either fact changes, so clearing the token on a 401 bounces
    // the app to the front door without any screen having to navigate.
    refreshListenable: _Refresh(ref),
    redirect: (context, state) {
      final ready = ref.read(hasServerProvider) && ref.read(isSignedInProvider);
      final atSignIn = state.matchedLocation == '/sign-in';
      if (!ready) return atSignIn ? null : '/sign-in';
      if (atSignIn) return '/';
      return null;
    },
    routes: [
      GoRoute(path: '/', builder: (_, _) => const HomeScreen()),
      GoRoute(path: '/sign-in', builder: (_, _) => const SignInScreen()),
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
