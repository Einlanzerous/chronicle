/// Criterion 10: no widget renders `SENT` unless the persisted state is
/// `acknowledged`. Exercised on the real [QueueScreen], not just on
/// `queue_label.dart`'s pure function -- this is what proves the screen
/// actually uses it.
///
/// **Every real filesystem call here goes through `tester.runAsync`.**
/// `testWidgets` runs its body in a fake-async test zone for deterministic
/// frame pumping, and genuine `dart:io` work -- a real temp directory, a
/// real file write -- never completes inside that zone; it hangs rather
/// than throwing, which is what made this worth a comment: the fix is
/// `runAsync`, not a different await.
library;

import 'dart:convert';
import 'dart:io';

import 'package:chronicle/api/providers.dart';
import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/features/queue/queue_screen.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_controller.dart';
import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'support/capture_fixture.dart';
import 'support/fake_chronicle_server.dart';
import 'support/no_recorder.dart';

void main() {
  late Directory root;

  tearDown(() async {
    if (await root.exists()) await root.delete(recursive: true);
  });

  Future<ProviderContainer> harness(
    WidgetTester tester,
    FakeChronicleServer server,
    FakeUploadTransport transport,
  ) async {
    late ProviderContainer container;
    await tester.runAsync(() async {
      root = await Directory.systemTemp.createTemp('chrn61-screen');
      SharedPreferences.setMockInitialValues(
        {'chronicle.server_url': 'https://chronicle-direct.example.com'},
      );
      final prefs = await SharedPreferences.getInstance();
      container = ProviderContainer(overrides: [
        prefsProvider.overrideWithValue(prefs),
        tokenStorageProvider.overrideWithValue(MemoryTokenStorage()),
        capturePlatformProvider.overrideWithValue(NoRecorderPlatform(root)),
        captureOwnerProvider.overrideWithValue(NoOwner()),
        queueEngineProvider.overrideWithValue(QueueEngine(transport: transport, chunkSize: 8)),
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
    return container;
  }

  testWidgets('an acknowledged capture reads SENT, and never before it actually is', (tester) async {
    final server = FakeChronicleServer();
    final transport = FakeUploadTransport(server);
    final container = await harness(tester, server, transport);
    addTearDown(container.dispose);

    await tester.runAsync(
      () => writeFixtureCapture(root, id: 'a', bytes: List.generate(16, (i) => i)),
    );

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: QueueScreen()),
      ),
    );
    // The first frame renders before any pass has run -- the capture reads
    // as absent-record, which queue_label.dart maps to QUEUED, never SENT.
    expect(find.text('SENT'), findsNothing);

    await tester.runAsync(() async {
      await container.read(captureControllerProvider.notifier).refresh();
      await container.read(queueControllerProvider.notifier).wake();
    });
    await tester.pump();

    expect(find.text('SENT'), findsOneWidget);
    expect(server.memos, hasLength(1));
  });

  testWidgets('a rejected capture never reads SENT, however long it sits', (tester) async {
    final server = FakeChronicleServer();
    final transport = FakeUploadTransport(server);
    transport.onBeforeCall = (call, {required isOpen}) => TransportFault.beforeSend(
          gen.ApiException(409, '{"code":"idempotency_key_reused","message":"nope"}'),
        );
    final container = await harness(tester, server, transport);
    addTearDown(container.dispose);

    await tester.runAsync(() => writeFixtureCapture(root, id: 'a', bytes: [1, 2, 3]));

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: QueueScreen()),
      ),
    );
    await tester.runAsync(() async {
      await container.read(captureControllerProvider.notifier).refresh();
      await container.read(queueControllerProvider.notifier).wake();
    });
    await tester.pump();

    expect(find.text('SENT'), findsNothing);
    expect(find.text('NOT SENT — KEY CONFLICT'), findsOneWidget);
  });

  testWidgets('tapping TRY AGAIN on a rejected row moves it back to pending and resends it',
      (tester) async {
    final server = FakeChronicleServer();
    final transport = FakeUploadTransport(server);
    transport.onBeforeCall = (call, {required isOpen}) => TransportFault.beforeSend(
          gen.ApiException(409, '{"code":"idempotency_key_reused","message":"nope"}'),
        );
    final container = await harness(tester, server, transport);
    addTearDown(container.dispose);

    await tester.runAsync(() => writeFixtureCapture(root, id: 'a', bytes: [1, 2, 3]));

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: QueueScreen()),
      ),
    );
    await tester.runAsync(() async {
      await container.read(captureControllerProvider.notifier).refresh();
      await container.read(queueControllerProvider.notifier).wake();
    });
    await tester.pump();
    expect(find.text('NOT SENT — KEY CONFLICT'), findsOneWidget);

    // The rejected row is the ONLY route back to pending -- the banner's
    // own RETRY calls wake(), which skips rejected captures entirely (see
    // the button's own build-time gating), so this is what the review
    // finding on PR #123 was about: without this control a rejected
    // capture was a dead end no screen could reach.
    transport.onBeforeCall = null;
    // The tap handler kicks off retryCapture()'s real file I/O
    // synchronously; per this file's own header note, that has to happen
    // inside runAsync too, or it hangs rather than throws.
    await tester.runAsync(() async {
      await tester.tap(find.text('TRY AGAIN'));
      await Future<void>.delayed(const Duration(milliseconds: 50));
    });
    await tester.pump();

    expect(find.text('SENT'), findsOneWidget);
    expect(find.text('NOT SENT — KEY CONFLICT'), findsNothing);
    expect(server.memos, hasLength(1));
  });
}
