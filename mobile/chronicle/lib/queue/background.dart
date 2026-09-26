/// The process-dead trigger: CHRN-61 ruling 1 (Dart engine + `workmanager`).
///
/// WorkManager starts a headless `FlutterEngine` on [queueCallbackDispatcher]
/// -- the SAME Dart entrypoint the foreground uses to drive the queue, so
/// there is exactly one implementation of the protocol, not a second one
/// re-derived in Kotlin.
///
/// **This isolate does not go through Riverpod.** There is no
/// `ProviderScope` here and building one would mean a second, divergent
/// path to the credential and server-address storage the foreground
/// already owns via `sessionTokenProvider`/`serverUrlProvider`. Instead it
/// reads `flutter_secure_storage`/`SharedPreferences` directly, at the
/// SAME keys those providers write to (`session.dart`'s `_tokenKey`,
/// `server_url.dart`'s `_serverKey`) -- so the two readings can never
/// disagree about where the truth lives even though they reach it through
/// different code. If either is unreadable, this yields cleanly
/// (`Result.success`, i.e. returns `true`) rather than crash-looping: a
/// headless isolate that repeatedly throws is a worse outcome than one
/// that quietly does nothing until the next scheduled attempt.
///
/// **Does not run `recoverAll`.** "Is anybody recording this?" is answered
/// by an in-process registry reachable only through `MainActivity`'s method
/// channel, which does not exist in this engine -- guessing wrong here
/// would risk trimming a live file. A capture torn by a reboot mid-recording
/// waits for the next launch to be recovered, same as the plan's own
/// "who wakes the queue" section describes.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:ui';

import 'package:chronicle_api/api.dart';
import 'package:crypto/crypto.dart' show sha256;
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:workmanager/workmanager.dart';

import '../api/transport.dart';
import '../capture/capture_record.dart';
import 'audio_gate_transport.dart';
import 'engine.dart';
import 'prune.dart';
import 'queue_record.dart';
import 'uploads_api_transport.dart';

/// Keys this file reads directly -- must stay byte-identical to
/// `lib/api/session.dart`'s `_tokenKey` and `lib/api/server_url.dart`'s
/// `_serverKey`. There is no shared constant between the three files
/// because `session.dart`/`server_url.dart` are Riverpod modules this
/// isolate deliberately does not import.
const _tokenStorageKey = 'chronicle.session_token';
const _serverUrlPrefsKey = 'chronicle.server_url';

const backgroundWakeTaskName = 'chronicle.queue.wake';
const backgroundWakeUniqueName = 'chronicle.queue.periodic';

/// The `inputData` key `main()` supplies the captures root path under. The
/// headless engine has no method channel to `MainActivity`, so it cannot
/// ask the platform where captures live the way the foreground does
/// (`CapturePlatform.capturesRoot`) -- the foreground tells it once, here,
/// at registration time instead. A reinstall or a data-dir change would be
/// covered by WorkManager re-registering this task at the app's next
/// launch, same as any other `registerPeriodicTask` call with
/// `ExistingPeriodicWorkPolicy.replace`.
const capturesRootInputKey = 'captures_root';

/// The name the foreground registers a port under while it exists
/// (`QueueController.build`/`dispose`), and this isolate checks for before
/// draining. Presence means the foreground is alive and will handle its
/// own triggers; absence means it is not, which is when this isolate's
/// work actually matters. This is the plan's own "ask the owner; an empty
/// registry is a definite answer" shape (`CaptureOwner`'s pattern),
/// applied to isolate-to-isolate presence rather than a platform channel.
///
/// **Not the safety property -- but no longer merely an optimisation either.**
/// If this check is ever wrong -- stale, racy, disabled outright for a test --
/// two engines draining the same capture still cannot produce a second memo or
/// an unverified ack (`engine.dart`'s own `_writeUnlessAcknowledged`,
/// `engine_concurrent_test.dart`). Before CHRN-120 that made the worst outcome
/// a wasted request and a 409 resync. The prune pass (`prune.dart`) DELETES, so
/// a second isolate can now find the file it was about to read gone: the
/// worst case is a real race, not a wasted request. It is handled -- the
/// engine reads a vanished file as "the file changed" and never writes over
/// an `acknowledged` record (`engine_prune_race_test.dart`) -- which is why the
/// handling exists, not why this check can be skipped.
const queueForegroundPortName = 'dev.dodson.chronicle.queue.foreground';

@pragma('vm:entry-point')
void queueCallbackDispatcher() {
  Workmanager().executeTask((task, inputData) async {
    if (task != backgroundWakeTaskName) return true;
    return _runBackgroundPass(inputData);
  });
}

Future<bool> _runBackgroundPass(Map<String, dynamic>? inputData) async {
  final rootPath = inputData?[capturesRootInputKey] as String?;
  if (rootPath == null || rootPath.isEmpty) return true;

  if (IsolateNameServer.lookupPortByName(queueForegroundPortName) != null) {
    return true; // the foreground is alive; its own triggers cover this pass
  }

  final String? token;
  final String? serverUrl;
  try {
    token = await const FlutterSecureStorage().read(key: _tokenStorageKey);
    final prefs = await SharedPreferences.getInstance();
    serverUrl = prefs.getString(_serverUrlPrefsKey);
  } catch (_) {
    // Credential-encrypted storage can be unreadable before first-unlock
    // after a reboot -- exactly the "reboot, not force-stopped" case the
    // plan's finding 7 names. Nothing to do yet; the next scheduled run
    // tries again.
    return true;
  }
  if (token == null || token.isEmpty || serverUrl == null || serverUrl.isEmpty) {
    return true;
  }

  final root = Directory(rootPath);
  if (!await root.exists()) return true;

  final apiClient = ApiClient(basePath: serverUrl)..client = ChronicleClient(token: token);
  await runBackgroundQueuePass(
    root: root,
    token: token,
    serverUrl: serverUrl,
    engine: QueueEngine(transport: UploadsApiTransport(UploadsApi(apiClient))),
    gate: MemosApiAudioGateTransport(MemosApi(apiClient)),
  );

  // Always Result.success: a capture that failed or got rejected is not a
  // WORKER failure, it is a fact this pass already recorded correctly.
  // WorkManager's own retry is for "this invocation could not run at
  // all", which returning false would ask for -- and asking again
  // immediately would not fix a 401 or a 5xx either.
  return true;
}

/// Everything a headless wake does once it has a token, an address and a
/// captures root: scan, drain, then prune. Split from [_runBackgroundPass] so
/// the WorkManager leg -- the one place that runs with nobody watching -- has a
/// test seam. `_runBackgroundPass` keeps the isolate-only concerns (the
/// foreground-presence check, secure storage, `SharedPreferences`) and builds
/// the real clients; this takes them as arguments, and `background_pass_test`
/// drives it with fakes.
///
/// The order is the rule: the prune pass runs immediately after `drainPass` and
/// never before it, so a capture is sent before anything is deleted, and
/// `prunePass` is skipped under the device block the drain reports.
Future<void> runBackgroundQueuePass({
  required Directory root,
  required String token,
  required String serverUrl,
  required QueueEngine engine,
  required AudioGateTransport gate,
  DateTime Function()? now,
  bool? pruneEnabled,
}) async {
  final clock = now ?? DateTime.now;
  final captures = <QueueCapture>[];
  await for (final entry in root.list()) {
    if (entry is! Directory) continue;
    final id = entry.path.split(Platform.pathSeparator).last;
    final captureDir = CaptureDir(root, id);
    final capture = await captureDir.readMeta();
    if (capture == null || capture.state == CaptureState.recording) continue;
    final queueDir = QueueDir(captureDir);
    // The same decision the foreground makes, from the same function: what
    // "no `upload.json`" means is `QueueDir.readOrEnqueue`'s to say.
    final record = await queueDir.readOrEnqueue(capture, clock());
    captures.add(QueueCapture(queueDir: queueDir, capture: capture, queueRecord: record));
  }
  if (captures.isEmpty) return;

  // No `currentBlock` carried in: this isolate is not durable across
  // invocations (the OS may kill it between runs, and there is nowhere to
  // persist a device block that is deliberately device-memory-only --
  // `device_block.dart`'s own rule). A block from a prior pass simply
  // re-derives itself from a fresh 401 if the reason for it still holds.
  final block = await engine.drainPass(
    captures: captures,
    token: token,
    serverUrl: serverUrl,
    tokenDigest: _digest(token),
    currentBlock: null,
  );

  await prunePass(
    captures: captures,
    transport: gate,
    deviceBlock: block,
    now: now,
    enabled: pruneEnabled,
  );
}

// Matches QueueController's own _digest exactly -- the same token must
// always produce the same digest, whichever isolate computes it.
String _digest(String token) => sha256.convert(utf8.encode(token)).toString();
