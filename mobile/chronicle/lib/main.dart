import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'api/reachability.dart';
import 'api/server_url.dart';
import 'api/session.dart';
import 'app.dart';

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

  runApp(
    UncontrolledProviderScope(
      container: container,
      child: const ChronicleApp(),
    ),
  );
}
