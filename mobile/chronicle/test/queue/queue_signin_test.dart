/// CHRN-170, on the real screens. `test/signin_from_capture_test.dart` proves
/// the tabs widget alone; this proves it is on CAPTURE and QUEUE, and that the
/// queue's own `SIGN IN TO SEND` banner carries the action it names.
library;

import 'dart:io';

import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/features/capture/capture_screen.dart';
import 'package:chronicle/features/queue/queue_screen.dart';
import 'package:chronicle/queue/device_block.dart';
import 'package:chronicle/queue/queue_controller.dart';
import 'package:chronicle/router/router.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';

import 'support/no_recorder.dart';

/// A queue controller that starts with a fixed device block and does nothing
/// else, so the banner can be tested without driving a 401 through an engine.
class _Blocked extends QueueController {
  _Blocked(this.block);
  final DeviceBlock? block;

  @override
  QueueUiState build() => QueueUiState(deviceBlock: block);
}

DeviceBlock _block(DeviceBlockReason reason) => DeviceBlock(
      reason: reason,
      serverUrl: 'https://chronicle-direct.example.com',
      tokenDigest: '',
    );

void main() {
  Future<GoRouter> pump(
    WidgetTester tester, {
    required String at,
    required bool ready,
    DeviceBlock? block,
  }) async {
    final root = Directory.systemTemp.createTempSync('chrn170');
    addTearDown(() => root.deleteSync(recursive: true));
    final router = GoRouter(
      initialLocation: at,
      routes: [
        GoRoute(path: queueRoute, builder: (_, _) => const QueueScreen()),
        GoRoute(path: captureRoute, builder: (_, _) => const CaptureScreen()),
        GoRoute(path: signInRoute, builder: (_, _) => const Text('front door')),
      ],
    );
    await tester.pumpWidget(ProviderScope(
      overrides: [
        capturePlatformProvider.overrideWithValue(NoRecorderPlatform(root)),
        captureOwnerProvider.overrideWithValue(NoOwner()),
        hasServerProvider.overrideWithValue(ready),
        isSignedInProvider.overrideWithValue(ready),
        queueControllerProvider.overrideWith(() => _Blocked(block)),
      ],
      child: MaterialApp.router(routerConfig: router),
    ));
    await tester.pump();
    return router;
  }

  String path(GoRouter r) => r.routerDelegate.currentConfiguration.uri.path;

  for (final at in [queueRoute, captureRoute]) {
    testWidgets('$at offers SIGN IN to a signed-out device and goes there',
        (tester) async {
      final router = await pump(tester, at: at, ready: false);
      expect(find.text('SIGN IN'), findsOneWidget);
      await tester.tap(find.text('SIGN IN'));
      await tester.pumpAndSettle();
      expect(path(router), signInRoute);
    });

    testWidgets('$at offers nothing to a signed-in device', (tester) async {
      await pump(tester, at: at, ready: true);
      expect(find.text('SIGN IN'), findsNothing);
    });
  }

  group('the queue banner', () {
    testWidgets('blocked as signed-out, it carries a SIGN IN action',
        (tester) async {
      // ready: true keeps the tab out of it, so this is the banner's button.
      final router = await pump(tester,
          at: queueRoute,
          ready: true,
          block: _block(DeviceBlockReason.signedOut));
      expect(find.text('Sign in to send what is held on this device.'),
          findsOneWidget);
      expect(find.text('SIGN IN'), findsOneWidget);
      await tester.tap(find.text('SIGN IN'));
      await tester.pumpAndSettle();
      expect(path(router), signInRoute);
    });

    testWidgets('blocked as the wrong host, it does not offer SIGN IN',
        (tester) async {
      await pump(tester,
          at: queueRoute,
          ready: true,
          block: _block(DeviceBlockReason.wrongHost));
      expect(find.textContaining('wrong host'), findsOneWidget);
      expect(find.text('SIGN IN'), findsNothing);
    });
  });
}
