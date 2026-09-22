/// The foreground wiring: launch/refresh triggers the queue, a device block
/// stops nothing else from working, a sign-in lifts it, and `/auth/me`
/// arbitrates a 401 exactly as the plan says it must.
library;

import 'dart:convert';
import 'dart:io';
import 'dart:ui';

import 'package:chronicle/api/providers.dart';
import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/queue/background.dart' show queueForegroundPortName;
import 'package:chronicle/queue/device_block.dart';
import 'package:chronicle/queue/engine.dart';
import 'package:chronicle/queue/queue_controller.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'support/capture_fixture.dart';
import 'support/fake_chronicle_server.dart';
import 'support/no_recorder.dart';

/// Waits for [condition] rather than sleeping a guessed duration -- the
/// same reasoning as `capture_controller_test.dart`'s own `_until`: several
/// of this controller's effects are unawaited by design (triggered by a
/// listener, not a call).
Future<void> _until(
  bool Function() condition, {
  Duration timeout = const Duration(seconds: 5),
  String? reason,
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    if (condition()) return;
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
  fail(reason ?? 'condition not met within $timeout');
}

void main() {
  // QueueController.build() constructs an AppLifecycleListener (the app-
  // resume trigger), which reaches WidgetsBinding.instance even though
  // nothing here pumps a widget tree.
  TestWidgetsFlutterBinding.ensureInitialized();

  late Directory root;
  late FakeChronicleServer server;
  late FakeUploadTransport transport;
  late ProviderContainer container;

  /// [meResponder] answers `GET /auth/me`; everything else 404s, since the
  /// queue itself is wired through [server]/[transport], not real HTTP.
  Future<void> setUpContainer({
    required http.Client meResponder,
    String? token = 'tok',
  }) async {
    root = await Directory.systemTemp.createTemp('chrn61-controller');
    server = FakeChronicleServer();
    transport = FakeUploadTransport(server);

    SharedPreferences.setMockInitialValues({'chronicle.server_url': 'https://chronicle-direct.example.com'});
    final prefs = await SharedPreferences.getInstance();

    container = ProviderContainer(overrides: [
      prefsProvider.overrideWithValue(prefs),
      tokenStorageProvider.overrideWithValue(MemoryTokenStorage()),
      capturePlatformProvider.overrideWithValue(NoRecorderPlatform(root)),
      captureOwnerProvider.overrideWithValue(NoOwner()),
      queueEngineProvider.overrideWithValue(QueueEngine(transport: transport, chunkSize: 8)),
      apiClientProvider.overrideWithValue(
        gen.ApiClient(basePath: 'https://chronicle-direct.example.com')..client = meResponder,
      ),
    ]);
    if (token != null) {
      await container.read(sessionTokenProvider.notifier).set(token);
    }
  }

  tearDown(() async {
    container.dispose();
    await Future<void>.delayed(Duration.zero);
    if (await root.exists()) await root.delete(recursive: true);
  });

  http.Client meIsFine(Map<String, Object?> user) => MockClient((request) async {
        if (request.url.path == '/auth/me') {
          return http.Response(jsonEncode(user), 200,
              headers: {'content-type': 'application/json'});
        }
        return http.Response('not found', 404);
      });

  const someUser = {
    'id': '11111111-1111-1111-1111-111111111111',
    'email': 'magos@example.test',
    'display_name': 'magos',
    'kind': 'person',
    'is_owner': true,
  };

  QueueController ctl() => container.read(queueControllerProvider.notifier);
  QueueUiState st() => container.read(queueControllerProvider);

  test('a ready capture on disk is acknowledged after wake()', () async {
    await setUpContainer(meResponder: meIsFine(someUser));
    await writeFixtureCapture(root, id: 'a', bytes: List.generate(20, (i) => i));

    await ctl().wake();

    expect(server.memos, hasLength(1));
    expect(st().records['a']?.status, QueueStatus.acknowledged);
  });

  test('a capture still recording is never touched', () async {
    await setUpContainer(meResponder: meIsFine(someUser));
    await writeFixtureCapture(
      root,
      id: 'a',
      bytes: List.generate(20, (i) => i),
      state: CaptureState.recording,
    );

    await ctl().wake();

    expect(server.memos, isEmpty);
    expect(st().records.containsKey('a'), isFalse);
  });

  test('a capture reaching ready via CaptureController.refresh wakes the queue unprompted',
      () async {
    await setUpContainer(meResponder: meIsFine(someUser));
    // The queue controller must exist (be built) before the capture
    // controller's refresh, exactly as it would from a widget watching
    // both -- reading it here is what registers its ref.listen.
    container.read(queueControllerProvider);

    await writeFixtureCapture(root, id: 'a', bytes: List.generate(20, (i) => i));
    await container.read(captureControllerProvider.notifier).refresh();

    await _until(
      () => container.read(queueControllerProvider).records['a']?.status ==
          QueueStatus.acknowledged,
      reason: 'the capture-ready listener should have woken the queue on its own',
    );
    expect(server.memos, hasLength(1));
  });

  test('a 401 blocks sending and asks /auth/me, which confirms the session is dead', () async {
    await setUpContainer(meResponder: MockClient((request) async {
      if (request.url.path == '/auth/me') return http.Response('{"code":"unauthorized"}', 401);
      return http.Response('not found', 404);
    }));
    transport.onBeforeCall = (call, {required isOpen}) =>
        TransportFault.beforeSend(gen.ApiException(401, '{"code":"unauthorized"}'));
    await writeFixtureCapture(root, id: 'a', bytes: [1, 2, 3]);

    await ctl().wake();

    expect(st().deviceBlock?.reason, DeviceBlockReason.signedOut);
    await _until(
      () => container.read(sessionTokenProvider) == null,
      reason: '/auth/me should have cleared the dead token',
    );
  });

  test('a transient 401 is lifted once /auth/me confirms the session is still good, and retries',
      () async {
    await setUpContainer(meResponder: meIsFine(someUser));
    var failNextOpen = true;
    transport.onBeforeCall = (call, {required isOpen}) {
      if (isOpen && failNextOpen) {
        failNextOpen = false;
        return TransportFault.beforeSend(gen.ApiException(401, '{"code":"unauthorized"}'));
      }
      return null;
    };
    await writeFixtureCapture(root, id: 'a', bytes: List.generate(20, (i) => i));

    // The arbitration is inline and deterministic: `wake()` raises the
    // block, invalidates and awaits `meProvider` itself, and -- since this
    // mock always confirms the session -- lifts it again before returning.
    // The block is therefore never observable from OUTSIDE this call; what
    // is observable is that it does not stick, and that the capture gets
    // retried.
    await ctl().wake();
    expect(st().deviceBlock, isNull, reason: 'a confirmed-good session lifts the block it raised');

    await _until(
      () => container.read(queueControllerProvider).records['a']?.status ==
          QueueStatus.acknowledged,
      reason: 'lifting the block should have retried the capture',
    );
  });

  test('signing back in resumes the queue unprompted', () async {
    await setUpContainer(meResponder: meIsFine(someUser), token: null);
    await writeFixtureCapture(root, id: 'a', bytes: List.generate(20, (i) => i));

    await ctl().wake();
    expect(st().deviceBlock?.reason, DeviceBlockReason.signedOut);
    expect(server.memos, isEmpty);

    await container.read(sessionTokenProvider.notifier).set('a-real-token');

    await _until(
      () => container.read(queueControllerProvider).records['a']?.status ==
          QueueStatus.acknowledged,
      reason: 'the sessionTokenProvider listener should have woken the queue',
    );
  });

  test('retryCapture moves a rejected capture back to pending and re-sends it', () async {
    await setUpContainer(meResponder: meIsFine(someUser));
    transport.onBeforeCall = (call, {required isOpen}) => TransportFault.beforeSend(gen.ApiException(
          409,
          '{"code":"idempotency_key_reused","message":"nope"}',
        ));
    await writeFixtureCapture(root, id: 'a', bytes: [1, 2, 3]);

    await ctl().wake();
    expect(st().records['a']?.status, QueueStatus.rejected);

    transport.onBeforeCall = null;
    await ctl().retryCapture('a');

    expect(st().records['a']?.status, QueueStatus.acknowledged);
    expect(server.memos, hasLength(1));
  });

  test('building the controller registers the isolate-presence port background.dart checks for',
      () async {
    // A standalone container, disposed directly here rather than through
    // the shared tearDown, so this test can observe both sides of the
    // lifecycle without double-disposing the shared one.
    final localRoot = await Directory.systemTemp.createTemp('chrn61-presence');
    SharedPreferences.setMockInitialValues({});
    final localPrefs = await SharedPreferences.getInstance();
    final localContainer = ProviderContainer(overrides: [
      prefsProvider.overrideWithValue(localPrefs),
      capturePlatformProvider.overrideWithValue(NoRecorderPlatform(localRoot)),
      captureOwnerProvider.overrideWithValue(NoOwner()),
      queueEngineProvider.overrideWithValue(QueueEngine(transport: FakeUploadTransport(FakeChronicleServer()))),
    ]);

    expect(
      IsolateNameServer.lookupPortByName(queueForegroundPortName),
      isNull,
      reason: 'nothing has built the controller yet',
    );

    localContainer.read(queueControllerProvider); // builds it
    expect(
      IsolateNameServer.lookupPortByName(queueForegroundPortName),
      isNotNull,
      reason: 'background.dart\'s headless dispatcher checks for exactly this presence',
    );

    localContainer.dispose();
    expect(
      IsolateNameServer.lookupPortByName(queueForegroundPortName),
      isNull,
      reason: 'a torn-down controller must not leave a stale "foreground is alive" signal',
    );

    if (await localRoot.exists()) await localRoot.delete(recursive: true);
  });
}
