/// CHRN-146: is this app exempt from battery optimisation, and the one action
/// that asks the system to make it so.
///
/// Why it matters: CHRN-143 measured that on a phone with Adaptive Battery the
/// periodic WorkManager job -- CHRN-61's process-dead upload retry and
/// CHRN-64's evening nudge -- was held in every unplugged sample unless the
/// package is on the device-idle whitelist. The exemption IS that whitelist.
///
/// **Nothing here ever blocks.** It is read, never required: capture, the
/// queue and triage behave identically whichever way it reads, the same rule
/// the notification permission follows. It is never asked at launch -- only
/// when the person taps the action on the queue screen -- and a refusal is
/// respected: the state stays visible and nothing re-asks.
library;

import 'dart:async';

import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

const MethodChannel _channel = MethodChannel('dev.dodson.chronicle/battery');

class BatteryPlatform {
  const BatteryPlatform();

  /// Null when the platform cannot say (not Android, or no handler): the UI
  /// treats that as "nothing to show", never as "not exempt".
  Future<bool?> isExempt() async {
    try {
      return await _channel.invokeMethod<bool>(
        'isIgnoringBatteryOptimizations',
      );
    } on MissingPluginException {
      return null;
    } on PlatformException {
      return null;
    }
  }

  /// Opens the system's request. Returns whether a screen opened; the answer
  /// is learnt by re-reading [isExempt] on resume.
  Future<bool> request() async {
    try {
      return await _channel.invokeMethod<bool>(
            'requestIgnoreBatteryOptimizations',
          ) ??
          false;
    } on MissingPluginException {
      return false;
    } on PlatformException {
      return false;
    }
  }
}

final batteryPlatformProvider = Provider<BatteryPlatform>(
  (ref) => const BatteryPlatform(),
);

/// `true` exempt, `false` restricted, `null` unknown (also the first frame).
class BatteryExemption extends Notifier<bool?> {
  AppLifecycleListener? _lifecycle;
  bool _alive = true;

  @override
  bool? build() {
    _alive = true;
    _lifecycle = AppLifecycleListener(onResume: () => unawaited(refresh()));
    ref.onDispose(() {
      _alive = false;
      _lifecycle?.dispose();
    });
    unawaited(refresh());
    return null;
  }

  Future<void> refresh() async {
    final exempt = await ref.read(batteryPlatformProvider).isExempt();
    if (_alive) state = exempt;
  }

  /// Opens the system request. Does not change [state] itself: the answer
  /// arrives as the app resumes, and [refresh] reads it then.
  /// Returns whether a screen opened.
  Future<bool> request() => ref.read(batteryPlatformProvider).request();
}

final batteryExemptionProvider = NotifierProvider<BatteryExemption, bool?>(
  BatteryExemption.new,
);
