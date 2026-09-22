import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:workmanager/workmanager.dart';

import 'api/reachability.dart';
import 'api/server_url.dart';
import 'api/session.dart';
import 'app.dart';
import 'capture/capture_controller.dart';
import 'queue/background.dart';
import 'queue/queue_controller.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  // Both of these are read before the first frame on purpose. The address lives
  // in SharedPreferences and the token in the keystore, and both reads are
  // asynchronous -- so a signed-in device that built the tree first would show
  // the sign-in screen for a frame and then be redirected off it, which reads as
  // a flicker at best and as "it logged me out" at worst.
  final prefs = await SharedPreferences.getInstance();

  final container = ProviderContainer(
    overrides: [prefsProvider.overrideWithValue(prefs)],
  );
  await container.read(sessionTokenProvider.notifier).load();

  // Fire the first probe without waiting for it: the status card renders
  // "Checking…" and fills in, rather than holding a blank screen against a
  // server that may be unreachable -- which is the ordinary case this app is
  // built for.
  if (container.read(hasServerProvider)) {
    container.read(reachabilityProvider.notifier).check();
  }

  // Recovery is a LAUNCH concern, not a screen concern, and this line is the
  // difference between the two.
  //
  // Building the controller is what runs it, and the controller is built when
  // something watches it -- which, before this, meant "when the capture screen
  // is opened". So a recording interrupted by a crash sat on disk as
  // `state: recording` until somebody happened to navigate to capture, and
  // criterion 5 failed on device for exactly that reason: relaunching after a
  // force-stop lands on home, and nothing recovered anything.
  //
  // Not awaited. It reads files and hashes bytes, and the first frame must not
  // wait for it -- the same reasoning as the probe above.
  //
  // The queue's own launch trigger is chained onto the END of recovery,
  // never run alongside it: recovery is what turns a capture torn by a
  // crash into `ready`/`salvaged`/`empty`, and the queue must never race it
  // to decide what is sendable. `QueueController` also re-scans on its own
  // whenever `CaptureController.recent` next changes, so this first chained
  // call is the launch case specifically, not the only trigger it has.
  unawaited(
    container
        .read(captureControllerProvider.notifier)
        .recover()
        .then((_) => container.read(queueControllerProvider.notifier).wake()),
  );

  // The process-dead trigger (CHRN-61 ruling 1): registers a periodic
  // WorkManager task on `queueCallbackDispatcher`, the same Dart entrypoint
  // the foreground drives the queue through. `update` refreshes the
  // captures-root `inputData` on every launch without disturbing an
  // already-scheduled cadence -- Android's own recommendation over
  // `replace`. Not awaited, for the same reason recovery and the probe
  // above are not: registering a task is not a first-frame concern, and a
  // force-stopped app never reaches this line at all (the platform fact
  // build step 5 and the plan's finding 7 both name), which is exactly
  // when nothing here is expected to run anyway.
  unawaited(_registerBackgroundWake(container));

  runApp(
    UncontrolledProviderScope(
      container: container,
      child: const ChronicleApp(),
    ),
  );
}

/// `NetworkType.connected` matches the plan's own choice: no point waking
/// under no connectivity at all, and the foreground's own reachability
/// probe already covers what happens while the app is open. A 15-minute
/// periodic run is WorkManager's own floor (`registerPeriodicTask`'s own
/// doc); the plan called this "a periodic safety net", not the primary
/// trigger -- the primary ones are the in-app triggers `QueueController`
/// already answers, plus this same task firing again whenever
/// connectivity actually returns.
Future<void> _registerBackgroundWake(ProviderContainer container) async {
  final root = await container.read(capturePlatformProvider).capturesRoot();
  await Workmanager().initialize(queueCallbackDispatcher);
  await Workmanager().registerPeriodicTask(
    backgroundWakeUniqueName,
    backgroundWakeTaskName,
    constraints: Constraints(networkType: NetworkType.connected),
    existingWorkPolicy: ExistingPeriodicWorkPolicy.update,
    inputData: {capturesRootInputKey: root.path},
  );
}
