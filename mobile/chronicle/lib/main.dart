import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api/reachability.dart';
import 'api/server_url.dart';
import 'api/session.dart';
import 'app.dart';
import 'capture/capture_controller.dart';
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

  runApp(
    UncontrolledProviderScope(
      container: container,
      child: const ChronicleApp(),
    ),
  );
}
