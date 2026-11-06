import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/features/shared/bottom_tabs.dart';
import 'package:chronicle/router/router.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

/// CHRN-170. `Record a memo` does `go('/capture')`, which leaves nothing to pop
/// to, and the CAPTURE/QUEUE tabs only switch between those two screens. A
/// device that recorded before signing in therefore had no way to the front
/// door. The tabs are on both screens, so one control there covers both.
void main() {
  Future<GoRouter> pump(WidgetTester tester, {required bool ready}) async {
    final router = GoRouter(
      initialLocation: queueRoute,
      routes: [
        GoRoute(
          path: queueRoute,
          builder: (_, _) =>
              const Scaffold(body: BottomCaptureQueueTabs(current: queueRoute)),
        ),
        GoRoute(path: signInRoute, builder: (_, _) => const Text('front door')),
        GoRoute(path: captureRoute, builder: (_, _) => const Text('capture')),
      ],
    );
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          hasServerProvider.overrideWithValue(ready),
          isSignedInProvider.overrideWithValue(ready),
        ],
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await tester.pumpAndSettle();
    return router;
  }

  testWidgets('a signed-out device can reach the front door from the tabs', (
    tester,
  ) async {
    final router = await pump(tester, ready: false);
    await tester.tap(find.text('SIGN IN'));
    await tester.pumpAndSettle();
    expect(router.routerDelegate.currentConfiguration.uri.path, signInRoute);
  });

  testWidgets('a signed-in device is not offered it', (tester) async {
    await pump(tester, ready: true);
    expect(find.text('SIGN IN'), findsNothing);
    expect(find.text('CAPTURE'), findsOneWidget);
  });
}
