/// CHRN-146 on the real queue screen, with the platform channel faked.
///
/// The faked thing is the channel itself (`dev.dodson.chronicle/battery`), so
/// the method names are pinned too. Anything that builds a QueueController
/// uses the queue fakes, so no test here reaches a real transport.
library;

import 'dart:convert';
import 'dart:io';

import 'package:chronicle/api/providers.dart';
import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/features/queue/queue_screen.dart';
import 'package:chronicle/power/battery_exemption.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_controller.dart';
import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../queue/support/capture_fixture.dart';
import '../queue/support/fake_chronicle_server.dart';
import '../queue/support/no_recorder.dart';

const _channel = MethodChannel('dev.dodson.chronicle/battery');

Future<void> _pumpUntil(WidgetTester tester, bool Function() condition) async {
  for (var i = 0; i < 80; i++) {
    if (condition()) return;
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(milliseconds: 25)),
    );
    await tester.pump();
  }
  expect(condition(), isTrue, reason: 'condition not met within 2s of polling');
}

void main() {
  late Directory root;
  late bool exempt;
  late List<String> calls;
  late bool canOpen;

  setUp(() {
    exempt = false;
    calls = [];
    canOpen = true;
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(_channel, (call) async {
          calls.add(call.method);
          switch (call.method) {
            case 'isIgnoringBatteryOptimizations':
              return exempt;
            case 'requestIgnoreBatteryOptimizations':
              return canOpen;
          }
          return null;
        });
  });

  tearDown(() async {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(_channel, null);
    if (await root.exists()) await root.delete(recursive: true);
  });

  Future<ProviderContainer> show(WidgetTester tester) async {
    late ProviderContainer container;
    final server = FakeChronicleServer();
    await tester.runAsync(() async {
      root = await Directory.systemTemp.createTemp('chrn146-screen');
      SharedPreferences.setMockInitialValues({
        'chronicle.server_url': 'https://chronicle-direct.example.com',
      });
      final prefs = await SharedPreferences.getInstance();
      container = ProviderContainer(
        overrides: [
          prefsProvider.overrideWithValue(prefs),
          tokenStorageProvider.overrideWithValue(MemoryTokenStorage()),
          capturePlatformProvider.overrideWithValue(NoRecorderPlatform(root)),
          captureOwnerProvider.overrideWithValue(NoOwner()),
          queueEngineProvider.overrideWithValue(
            QueueEngine(transport: FakeUploadTransport(server), chunkSize: 8),
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
        ],
      );
      await container.read(sessionTokenProvider.notifier).set('tok');
    });
    addTearDown(container.dispose);
    // Runs before dispose: let any pass the queue started finish first.
    addTearDown(
      () => tester.runAsync(
        () => container.read(queueControllerProvider.notifier).wake(),
      ),
    );
    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: QueueScreen()),
      ),
    );
    return container;
  }

  testWidgets('exempt: nothing is shown', (tester) async {
    exempt = true;
    await show(tester);
    await _pumpUntil(
      tester,
      () => calls.contains('isIgnoringBatteryOptimizations'),
    );
    await tester.pump();
    expect(find.text('BATTERY LIMITED'), findsNothing);
    expect(find.text('ALLOW'), findsNothing);
  });

  testWidgets(
    'not exempt: the indication and the action appear, 44 px tall at least',
    (tester) async {
      await show(tester);
      await _pumpUntil(
        tester,
        () => find.text('BATTERY LIMITED').evaluate().isNotEmpty,
      );
      expect(find.text('ALLOW'), findsOneWidget);
      final size = tester.getSize(find.widgetWithText(TextButton, 'ALLOW'));
      expect(size.height, greaterThanOrEqualTo(44));
      expect(size.width, greaterThanOrEqualTo(44));
    },
  );

  testWidgets('tapping ALLOW invokes the system request', (tester) async {
    await show(tester);
    await _pumpUntil(tester, () => find.text('ALLOW').evaluate().isNotEmpty);
    expect(calls, isNot(contains('requestIgnoreBatteryOptimizations')));
    await tester.tap(find.text('ALLOW'));
    await _pumpUntil(
      tester,
      () => calls.contains('requestIgnoreBatteryOptimizations'),
    );
  });

  testWidgets(
    'a refusal keeps the indication, does not ask again, and leaves the queue working',
    (tester) async {
      final container = await show(tester);
      await tester.runAsync(
        () => writeFixtureCapture(
          root,
          id: 'a',
          bytes: List.generate(16, (i) => i),
        ),
      );
      await _pumpUntil(tester, () => find.text('ALLOW').evaluate().isNotEmpty);
      await tester.tap(find.text('ALLOW'));
      await _pumpUntil(
        tester,
        () => calls.contains('requestIgnoreBatteryOptimizations'),
      );

      // The person said no: the system leaves the state as it was, and the app
      // comes back to the foreground.
      await tester.runAsync(
        () => container.read(batteryExemptionProvider.notifier).refresh(),
      );
      await tester.pump();
      expect(
        find.text('BATTERY LIMITED'),
        findsOneWidget,
        reason: 'the state stays visible',
      );
      expect(
        calls.where((c) => c == 'requestIgnoreBatteryOptimizations'),
        hasLength(1),
        reason: 'one request per tap, never a nag',
      );

      // The queue still sends.
      await tester.runAsync(() async {
        await container.read(captureControllerProvider.notifier).refresh();
        await container.read(queueControllerProvider.notifier).wake();
      });
      await _pumpUntil(tester, () => find.text('SENT').evaluate().isNotEmpty);
      expect(find.text('SENT'), findsOneWidget);
    },
  );

  testWidgets(
    'a phone with nowhere to send the request says so, and nothing else changes',
    (tester) async {
      canOpen = false;
      await show(tester);
      await _pumpUntil(tester, () => find.text('ALLOW').evaluate().isNotEmpty);
      await tester.tap(find.text('ALLOW'));
      await _pumpUntil(
        tester,
        () => find
            .textContaining('no screen the app can open')
            .evaluate()
            .isNotEmpty,
      );
      expect(find.text('BATTERY LIMITED'), findsOneWidget);
    },
  );

  testWidgets('accepting clears the indication once the app is back', (
    tester,
  ) async {
    final container = await show(tester);
    await _pumpUntil(tester, () => find.text('ALLOW').evaluate().isNotEmpty);
    await tester.tap(find.text('ALLOW'));
    await _pumpUntil(
      tester,
      () => calls.contains('requestIgnoreBatteryOptimizations'),
    );

    exempt = true;
    await tester.runAsync(
      () => container.read(batteryExemptionProvider.notifier).refresh(),
    );
    await tester.pump();
    expect(find.text('BATTERY LIMITED'), findsNothing);
  });

  testWidgets(
    'a platform that cannot answer shows nothing rather than "not exempt"',
    (tester) async {
      TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
          .setMockMethodCallHandler(_channel, null);
      await show(tester);
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(milliseconds: 50)),
      );
      await tester.pump();
      expect(find.text('BATTERY LIMITED'), findsNothing);
    },
  );
}
