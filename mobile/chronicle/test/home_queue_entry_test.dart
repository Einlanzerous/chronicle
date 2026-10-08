/// CHRN-142: Home has an entry to the queue, so "did my memos send?" does not
/// need a trip through the recorder.
///
/// Built on the queue fakes in `test/queue/support`: the real [QueueScreen]
/// sits behind the route, so no real transport is reachable. Filesystem work
/// goes through `runAsync` and waits poll, as in `queue_screen_test.dart`.
library;

import 'dart:convert';
import 'dart:io';

import 'package:chronicle/api/providers.dart';
import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/features/home/home_screen.dart';
import 'package:chronicle/features/queue/queue_screen.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_controller.dart';
import 'package:chronicle/router/router.dart';
import 'package:chronicle/theme/tokens.dart';
import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'queue/support/fake_chronicle_server.dart';
import 'queue/support/no_recorder.dart';

Future<void> _pumpUntil(WidgetTester tester, bool Function() condition) async {
  for (var i = 0; i < 80; i++) {
    if (condition()) return;
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 25)));
    await tester.pump();
  }
  expect(condition(), isTrue, reason: 'condition not met within 2s of polling');
}

void main() {
  late Directory root;

  tearDown(() async {
    if (await root.exists()) await root.delete(recursive: true);
  });

  Future<(ProviderContainer, GoRouter)> pumpHome(WidgetTester tester) async {
    late ProviderContainer container;
    await tester.runAsync(() async {
      root = await Directory.systemTemp.createTemp('chrn142-home');
      SharedPreferences.setMockInitialValues(
        {'chronicle.server_url': 'https://chronicle-direct.example.com'},
      );
      final prefs = await SharedPreferences.getInstance();
      container = ProviderContainer(overrides: [
        prefsProvider.overrideWithValue(prefs),
        tokenStorageProvider.overrideWithValue(MemoryTokenStorage()),
        capturePlatformProvider.overrideWithValue(NoRecorderPlatform(root)),
        captureOwnerProvider.overrideWithValue(NoOwner()),
        queueEngineProvider.overrideWithValue(
          QueueEngine(transport: FakeUploadTransport(FakeChronicleServer()), chunkSize: 8),
        ),
        apiClientProvider.overrideWithValue(
          gen.ApiClient(basePath: 'https://chronicle-direct.example.com')
            ..client = MockClient((request) async {
              if (request.url.path == '/auth/me') {
                return http.Response(
                  jsonEncode({
                    'id': '11111111-1111-1111-1111-111111111111',
                    'email': 'magos@example.test',
                    'display_name': 'magos',
                    'kind': 'person',
                    'is_owner': true,
                  }),
                  200,
                  headers: {'content-type': 'application/json'},
                );
              }
              return http.Response('not found', 404);
            }),
        ),
      ]);
      await container.read(sessionTokenProvider.notifier).set('tok');
    });
    addTearDown(container.dispose);

    // Home at '/', the real queue screen at queueRoute: the same shape as
    // router.dart, minus the redirect, which is router_test.dart's.
    final router = GoRouter(
      routes: [
        GoRoute(path: '/', builder: (_, _) => const HomeScreen()),
        GoRoute(path: queueRoute, builder: (_, _) => const QueueScreen()),
      ],
    );
    addTearDown(router.dispose);

    tester.view.physicalSize = const Size(800, 1800);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: MaterialApp.router(routerConfig: router),
      ),
    );
    await _pumpUntil(tester, () => find.byKey(const ValueKey('home-queue')).evaluate().isNotEmpty);
    return (container, router);
  }

  testWidgets('tapping the Queue entry lands on the queue screen', (tester) async {
    final (_, router) = await pumpHome(tester);

    expect(find.byType(QueueScreen), findsNothing);
    await tester.tap(find.byKey(const ValueKey('home-queue')));
    await _pumpUntil(tester, () => find.byType(QueueScreen).evaluate().isNotEmpty);

    expect(router.routerDelegate.currentConfiguration.last.matchedLocation, queueRoute);
    expect(find.byType(QueueScreen), findsOneWidget);
  });

  testWidgets('the system back from the queue returns to Home', (tester) async {
    final (_, router) = await pumpHome(tester);

    await tester.tap(find.byKey(const ValueKey('home-queue')));
    await _pumpUntil(tester, () => find.byType(QueueScreen).evaluate().isNotEmpty);

    await tester.binding.handlePopRoute();
    await _pumpUntil(tester, () => find.byType(QueueScreen).evaluate().isEmpty);

    expect(router.routerDelegate.currentConfiguration.last.matchedLocation, '/');
    expect(find.byType(HomeScreen), findsOneWidget);
  });

  testWidgets('the Queue entry is at least 44 px tall', (tester) async {
    await pumpHome(tester);

    final size = tester.getSize(find.byKey(const ValueKey('home-queue')));
    expect(size.height, greaterThanOrEqualTo(minTapTarget));
    expect(minTapTarget, greaterThanOrEqualTo(44.0));
  });
}
