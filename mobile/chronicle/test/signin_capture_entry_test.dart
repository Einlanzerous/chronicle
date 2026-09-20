import 'package:chronicle/features/signin/sign_in_screen.dart';
import 'package:chronicle/router/router.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

/// The front door has to offer a way to capture, or "sign-in gates sending,
/// never capturing" is true of the router and false of the app.
///
/// The router already permits `/capture` without a credential
/// (`router_test.dart`), and this is the other half: that a device sent to the
/// front door can actually *get* there. Before CHRN-60's device pass it could
/// not — the redirect allowed the route and nothing navigated to it, so a
/// revoked session meant no recording at all.
void main() {
  testWidgets('the sign-in screen offers a way to record without a credential',
      (tester) async {
    final router = GoRouter(
      initialLocation: '/sign-in',
      routes: [
        GoRoute(path: '/sign-in', builder: (_, _) => const SignInScreen()),
        GoRoute(
          path: captureRoute,
          builder: (_, _) => const Scaffold(body: Text('capture')),
        ),
      ],
    );

    await tester.pumpWidget(
      ProviderScope(child: MaterialApp.router(routerConfig: router)),
    );
    await tester.pumpAndSettle();

    final entry = find.widgetWithText(TextButton, 'Record a memo');
    expect(entry, findsOneWidget,
        reason: 'a device with no credential must have a route to capture');

    // The sign-in screen scrolls, and on a short test viewport this sits below
    // the fold -- which is also a real thing to know about the screen.
    await tester.ensureVisible(entry);
    await tester.pumpAndSettle();
    await tester.tap(entry);
    await tester.pumpAndSettle();

    expect(router.routerDelegate.currentConfiguration.uri.path, captureRoute);
  });

  testWidgets('and says what happens to a memo recorded before signing in',
      (tester) async {
    await tester.pumpWidget(
      const ProviderScope(child: MaterialApp(home: SignInScreen())),
    );
    await tester.pumpAndSettle();

    // Not a detail: a memo that records and then sits is only trustworthy if
    // the screen said it would. CHRN-61 shows the same fact in the queue.
    expect(find.textContaining('held on'), findsOneWidget);
  });
}
