/// CHRN-62 on the real screens: the confirm after recording, on the capture
/// screen, and the same choice from a queue row.
///
/// What these prove is the Done-when's shape against a faithful fake server
/// (`support/fake_chronicle_server.dart`): the choice reaches the server as
/// the declaration, a skip reaches it as no opinion, and an undecided
/// capture reaches it not at all.
///
/// Real filesystem calls go through `tester.runAsync` -- see
/// `queue_screen_test.dart`'s own header note for why.
library;

import 'dart:convert';
import 'dart:io';

import 'package:chronicle/api/providers.dart';
import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/features/capture/capture_screen.dart';
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

import '../queue/support/capture_fixture.dart';
import '../queue/support/fake_chronicle_server.dart';
import '../queue/support/no_recorder.dart';

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
  late FakeChronicleServer server;
  late ProviderContainer container;

  tearDown(() async {
    if (await root.exists()) await root.delete(recursive: true);
  });

  /// The canvas's frame, so the card is laid out where it will really be.
  void phone(WidgetTester tester) {
    tester.view.physicalSize = const Size(412, 915);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
  }

  Future<void> start(WidgetTester tester, Widget screen) async {
    phone(tester);
    server = FakeChronicleServer();
    await tester.runAsync(() async {
      root = await Directory.systemTemp.createTemp('chrn62-screen');
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
      ]);
      await container.read(sessionTokenProvider.notifier).set('tok');
      // Just recorded, and nobody has chosen anything yet.
      await writeFixtureCapture(
        root,
        id: 'a',
        bytes: List.generate(16, (i) => i),
        startedAt: DateTime.now(),
        undecided: true,
      );
    });
    addTearDown(container.dispose);
    // Registered after dispose, so it runs BEFORE it (tear-downs run last
    // first): a decision wakes the queue on the real event loop, and a pass
    // still writing upload.json when the temp directory is deleted throws
    // after the test has finished. wake() coalesces onto any pass in flight
    // and returns once the queue is quiet.
    addTearDown(() => tester.runAsync(
          () => container.read(queueControllerProvider.notifier).wake(),
        ));

    await tester.pumpWidget(
      UncontrolledProviderScope(container: container, child: MaterialApp(home: screen)),
    );
    await tester.runAsync(() async {
      await container.read(captureControllerProvider.notifier).refresh();
      await container.read(queueControllerProvider.notifier).wake();
    });
    await tester.pump();
  }

  Future<CaptureRecord?> meta() => CaptureDir(root, 'a').readMeta();

  group('the capture screen, right after recording', () {
    testWidgets('an undecided capture shows the card, is held, and never reaches the server',
        (tester) async {
      await start(tester, const CaptureScreen());

      expect(find.text('THE AUDIO'), findsOneWidget);
      expect(find.text('CONFIRM · 30 DAYS'), findsOneWidget, reason: '30 DAYS is preselected');
      expect(find.text('TRANSCRIPT IS THE DURABLE ARTEFACT'), findsOneWidget);
      expect(find.text('AUDIO PRUNES AT 30 DAYS'), findsOneWidget);
      expect(find.text('AWAITING RETENTION'), findsOneWidget);
      // The card never covers the control that starts the next memo.
      expect(find.byIcon(Icons.mic_none), findsOneWidget);
      expect(server.declaredRetentions, isEmpty);
      expect(tester.takeException(), isNull, reason: 'no overflow at 412 x 915');
    });

    testWidgets('CONFIRM on the preselected chip is the one-tap fast path to 30 days',
        (tester) async {
      await start(tester, const CaptureScreen());

      await tester.runAsync(() => tester.tap(find.text('CONFIRM · 30 DAYS')));
      await _pumpUntil(tester, () => server.declaredRetentions.isNotEmpty);

      expect(server.declaredRetentions, ['days_30']);
      await _pumpUntil(tester, () => find.text('THE AUDIO').evaluate().isEmpty);
      // The server holds the declaration before the ack is back and the
      // queue's state reaches the screen, so this waits too, never assumes.
      await _pumpUntil(tester, () => find.text('SENT').evaluate().isNotEmpty);
      expect(find.text('SENT'), findsOneWidget);
    });

    testWidgets('DISCARD NOW is not the same tap: the chip selects, CONFIRM commits',
        (tester) async {
      await start(tester, const CaptureScreen());

      await tester.runAsync(() => tester.tap(find.text('DISCARD NOW')));
      await tester.pump();
      expect(find.text('CONFIRM · DISCARD NOW'), findsOneWidget);
      expect(find.text('Discard once transcribed'), findsOneWidget);
      await tester.runAsync(() async {
        expect((await meta())!.retention, isNull, reason: 'a chip alone writes nothing');
      });
      expect(server.declaredRetentions, isEmpty);

      await tester.runAsync(() => tester.tap(find.text('CONFIRM · DISCARD NOW')));
      await _pumpUntil(tester, () => server.declaredRetentions.isNotEmpty);
      expect(server.declaredRetentions, ['discard_now']);
      await tester.runAsync(() async {
        expect((await meta())!.retention, 'discard_now');
      });
    });

    testWidgets('FOREVER reaches the server as forever', (tester) async {
      await start(tester, const CaptureScreen());
      await tester.runAsync(() => tester.tap(find.text('FOREVER')));
      await tester.pump();
      await tester.runAsync(() => tester.tap(find.text('CONFIRM · FOREVER')));
      await _pumpUntil(tester, () => server.declaredRetentions.isNotEmpty);
      expect(server.declaredRetentions, ['forever']);
    });

    testWidgets('SKIP sends at once with no opinion -- the server\'s 30-day default',
        (tester) async {
      await start(tester, const CaptureScreen());

      await tester.runAsync(() => tester.tap(find.text('SKIP')));
      await _pumpUntil(tester, () => server.declaredRetentions.isNotEmpty);

      expect(server.declaredRetentions, [null]);
      // Declared is not yet finalised: the chunks and the completion follow.
      await _pumpUntil(tester, () => server.memos.isNotEmpty);
      expect(server.memos.single.retention, gen.MemoRetentionEnum.days30);
      await tester.runAsync(() async {
        final record = (await meta())!;
        expect(record.retention, isNull);
        expect(record.retentionSkippedAt, isNotNull);
      });
    });
  });

  group('the queue screen', () {
    testWidgets('an undecided row offers CHOOSE, and the choice from it is what is declared',
        (tester) async {
      await start(tester, const QueueScreen());

      expect(find.text('AWAITING RETENTION'), findsOneWidget);
      expect(find.text('AUDIO · NOT YET CHOSEN'), findsOneWidget);
      expect(find.text('TRANSCRIPT IS THE DURABLE ARTEFACT'), findsOneWidget);

      await tester.tap(find.text('CHOOSE'));
      await tester.pumpAndSettle();
      expect(find.text('THE AUDIO'), findsOneWidget);

      await tester.runAsync(() => tester.tap(find.text('FOREVER')));
      await tester.pump();
      await tester.runAsync(() => tester.tap(find.text('CONFIRM · FOREVER')));
      await _pumpUntil(tester, () => server.declaredRetentions.isNotEmpty);
      await tester.pumpAndSettle();

      expect(server.declaredRetentions, ['forever']);
      expect(find.text('THE AUDIO'), findsNothing, reason: 'the sheet closes on a decision');
      expect(find.text('CHOOSE'), findsNothing, reason: 'the choice is made once');
      expect(find.text('AUDIO · FOREVER'), findsOneWidget);
    });
  });
}
