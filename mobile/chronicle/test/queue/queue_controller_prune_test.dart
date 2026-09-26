/// The foreground's half of CHRN-120: the prune leg's place in a wake, and the
/// two changes to `wake()` and to the scan that a deleting pass made necessary.
///
/// * **The prune leg follows the drain**, and is a no-op in a build that has not
///   opted in.
/// * **A wake that arrives while a pass is running is not swallowed.**
///   `wake()` coalesces concurrent calls onto the running pass, and a pass
///   scans the captures directory once, at its start -- so a capture that
///   reached `ready` a moment later waited for the next unrelated trigger. The
///   prune leg (up to ten round trips after the drain) made that window long
///   enough to matter.
/// * **A lost `upload.json` never re-enqueues a pruned capture.**
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:chronicle/api/providers.dart';
import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/capture/capture_controller.dart';
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
import 'support/fake_audio_gate.dart';
import 'support/fake_chronicle_server.dart';
import 'support/no_recorder.dart';

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
  TestWidgetsFlutterBinding.ensureInitialized();

  late Directory root;
  late FakeChronicleServer server;
  late FakeUploadTransport transport;
  late FakeAudioGate gate;
  late ProviderContainer container;

  Future<void> setUpContainer({required bool prune}) async {
    root = await Directory.systemTemp.createTemp('chrn120-controller');
    server = FakeChronicleServer();
    transport = FakeUploadTransport(server);
    gate = FakeAudioGate();

    SharedPreferences.setMockInitialValues(
        {'chronicle.server_url': 'https://chronicle-direct.example.com'});
    final prefs = await SharedPreferences.getInstance();

    container = ProviderContainer(overrides: [
      prefsProvider.overrideWithValue(prefs),
      tokenStorageProvider.overrideWithValue(MemoryTokenStorage()),
      capturePlatformProvider.overrideWithValue(NoRecorderPlatform(root)),
      captureOwnerProvider.overrideWithValue(NoOwner()),
      queueEngineProvider.overrideWithValue(QueueEngine(transport: transport, chunkSize: 8)),
      audioGateTransportProvider.overrideWithValue(gate),
      if (prune) pruneEnabledProvider.overrideWithValue(true),
      apiClientProvider.overrideWithValue(
        gen.ApiClient(basePath: 'https://chronicle-direct.example.com')
          ..client = MockClient((_) async => http.Response(jsonEncode({}), 404)),
      ),
    ]);
    await container.read(sessionTokenProvider.notifier).set('tok');
  }

  tearDown(() async {
    container.dispose();
    await Future<void>.delayed(Duration.zero);
    if (await root.exists()) await root.delete(recursive: true);
  });

  QueueController ctl() => container.read(queueControllerProvider.notifier);
  QueueUiState st() => container.read(queueControllerProvider);
  final bytes = List<int>.generate(20, (i) => i);

  test('a build that has not opted in sends, and the prune leg asks nothing',
      () async {
    await setUpContainer(prune: false);
    final qc = await writeFixtureCapture(root, id: 'a', bytes: bytes, retention: 'discard_now');
    gate.fallback = Answers.pruned;

    await ctl().wake();

    expect(server.memos, hasLength(1));
    expect(gate.probed, isEmpty);
    expect(await qc.queueDir.capture.audio.exists(), isTrue);
  });

  test('an opted-in build sends first, then deletes on the server\'s word', () async {
    await setUpContainer(prune: true);
    final qc = await writeFixtureCapture(root, id: 'a', bytes: bytes, retention: 'discard_now');
    gate.fallback = Answers.pruned;

    await ctl().wake();

    expect(server.memos, hasLength(1));
    expect(st().records['a']?.status, QueueStatus.acknowledged);
    expect(gate.probed, hasLength(1));
    expect(await qc.queueDir.capture.audio.exists(), isFalse);
    expect(await qc.queueDir.capture.pruned.exists(), isTrue);
  });

  test('an unacknowledged capture is never asked about, even alongside one that is',
      () async {
    await setUpContainer(prune: true);
    // 'a' will be delivered; 'p' cannot be (the fake server refuses new
    // sessions), so it stays pending and must never reach the gate.
    await writeFixtureCapture(root, id: 'a', bytes: bytes, retention: 'discard_now');
    gate.fallback = Answers.stillThere;
    await ctl().wake();
    server.forcePendingLimit = true;
    final pending = await writeFixtureCapture(root, id: 'p', bytes: bytes);
    gate.probed.clear();

    await ctl().wake();

    expect(st().records['p']?.status, QueueStatus.pending,
        reason: 'the fake server refused it, so it is genuinely undelivered');
    // 'a' was asked about in the first wake and is not due again for 24 hours;
    // 'p' has no memo to ask about. Nothing reached the gate.
    expect(gate.probed, isEmpty);
    expect(await pending.queueDir.capture.audio.exists(), isTrue);
  });

  test('a wake that arrives during the prune leg is not swallowed', () async {
    await setUpContainer(prune: true);
    await writeFixtureCapture(root, id: 'a', bytes: bytes, retention: 'discard_now');
    final hold = Completer<void>();
    gate.onProbe = (_) => hold.future;

    final first = ctl().wake();
    // 'a' has been delivered and the pass is now inside the prune leg.
    await _until(() => gate.probed.isNotEmpty, reason: 'the prune leg never started');
    expect(server.memos, hasLength(1));

    // A capture reaches `ready` while that pass is still running. The pass
    // scanned the directory before this file existed.
    await writeFixtureCapture(root, id: 'b', bytes: bytes);
    final second = ctl().wake(); // coalesces onto the running pass
    hold.complete();
    await Future.wait([first, second]);

    expect(server.memos, hasLength(2),
        reason: 'the late capture was sent by the re-run, with no further trigger');
    expect(st().records['b']?.status, QueueStatus.acknowledged);
  });

  test('wakes during a pass cause at most one more pass, not a loop', () async {
    await setUpContainer(prune: true);
    await writeFixtureCapture(root, id: 'a', bytes: bytes, retention: 'discard_now');
    final hold = Completer<void>();
    gate.onProbe = (_) => hold.future;

    final first = ctl().wake();
    await _until(() => gate.probed.isNotEmpty);
    final callsAtHold = transport.calls;
    // Five triggers while the pass runs collapse into one re-run.
    final rest = [for (var i = 0; i < 5; i++) ctl().wake()];
    hold.complete();
    await Future.wait([first, ...rest]);

    // The re-run found nothing left to send: the delivered capture is
    // acknowledged and 'a' was already asked about, so nothing more happened.
    expect(transport.calls, callsAtHold);
    expect(server.memos, hasLength(1));
  });

  test('a lost upload.json does not re-enqueue a pruned capture', () async {
    await setUpContainer(prune: true);
    final qc = await writeFixtureCapture(root, id: 'a', bytes: bytes, retention: 'discard_now');
    gate.fallback = Answers.pruned;
    await ctl().wake();
    expect(await qc.queueDir.capture.audio.exists(), isFalse);
    final memoId = server.memos.single.id;

    await qc.queueDir.uploadFile.delete();
    final callsBefore = transport.calls;
    await ctl().wake();

    expect(transport.calls, callsBefore, reason: 'nothing was re-opened or re-sent');
    expect(server.memos, hasLength(1));
    final record = (await qc.queueDir.read())!;
    expect(record.status, QueueStatus.acknowledged);
    expect(record.memoId, memoId);
    expect(st().records['a']?.status, QueueStatus.acknowledged,
        reason: 'and the screen says SENT, not QUEUED or FILE CHANGED');
  });
}
