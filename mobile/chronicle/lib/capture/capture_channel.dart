/// The Dart half of the seam to `CaptureService`.
///
/// Everything here is something Dart cannot know on its own. In particular
/// [PlatformCaptureOwner] is not a convenience wrapper: it is the answer to
/// *"is a live recorder still writing this capture?"*, read straight out of the
/// service's in-process registry, and it is what lets recovery act immediately
/// after a kill instead of waiting out a lease.
library;

import 'dart:io';

import 'package:flutter/services.dart';

import 'recovery.dart';

const MethodChannel _methods = MethodChannel('dev.dodson.chronicle/capture');
const EventChannel _events = EventChannel('dev.dodson.chronicle/capture/events');

/// What the recorder is doing, as the platform sees it.
enum RecorderState { idle, recording, paused }

/// A flat read of the recorder, as of the moment it was taken.
class RecorderSnapshot {
  const RecorderSnapshot({
    required this.captureId,
    required this.state,
    required this.elapsedMs,
    required this.byteSize,
    required this.silenced,
    required this.micOpen,
    required this.amplitude,
  });

  final String? captureId;
  final RecorderState state;

  /// From `SystemClock.elapsedRealtime()`, minus paused time.
  ///
  /// Never wall clock: `MediaRecorder` has no position getter to borrow, and a
  /// memo that appears to run backwards because NTP corrected the device
  /// mid-recording is a small bug with a very bad look.
  final int elapsedMs;

  /// The real size of the file on disk.
  ///
  /// Shown as `LOCAL BUFFER` rather than estimated from elapsed x bitrate,
  /// because it is the one number on the screen that proves the invariant to
  /// the person holding the phone — and an estimate would keep counting up
  /// after the disk stopped accepting writes.
  final int byteSize;

  /// Something else has taken the microphone and this recorder is writing
  /// zeros. Android silences rather than refuses, and raises nothing.
  final bool silenced;

  /// The recorder's own `AudioRecordingMonitor` reports an active,
  /// non-silenced configuration.
  ///
  /// **This, and not `start()` returning, is what RECORDING means.** A screen
  /// claiming to record while the microphone is silenced is the same class of
  /// lie as a queue showing a memo as sent.
  final bool micOpen;

  final int amplitude;

  static const idle = RecorderSnapshot(
    captureId: null,
    state: RecorderState.idle,
    elapsedMs: 0,
    byteSize: 0,
    silenced: false,
    micOpen: false,
    amplitude: 0,
  );

  static RecorderSnapshot fromMap(Map<Object?, Object?> map) => RecorderSnapshot(
        captureId: map['captureId'] as String?,
        state: RecorderState.values.firstWhere(
          (s) => s.name == map['state'],
          orElse: () => RecorderState.idle,
        ),
        elapsedMs: (map['elapsedMs'] as int?) ?? 0,
        byteSize: (map['byteSize'] as int?) ?? 0,
        silenced: (map['silenced'] as bool?) ?? false,
        micOpen: (map['micOpen'] as bool?) ?? false,
        amplitude: (map['amplitude'] as int?) ?? 0,
      );
}

/// How the recorder should behave when something takes the microphone.
///
/// The names match `CaptureService.InterruptionPolicy`. All three exist because
/// CHRN-60's ruling pre-committed its own fallbacks: pause-and-resume is the
/// approved behaviour, and if a paused recorder turns out to report no
/// configuration at all — leaving nothing to resume on — the manual variant
/// applies without another decision.
enum InterruptionPolicy { pauseResume, pauseManual, stop }

extension on InterruptionPolicy {
  String get wire => switch (this) {
        InterruptionPolicy.pauseResume => 'PAUSE_RESUME',
        InterruptionPolicy.pauseManual => 'PAUSE_MANUAL',
        InterruptionPolicy.stop => 'STOP',
      };
}

class CapturePlatform {
  const CapturePlatform();

  /// `<filesDir>/captures`, asked for rather than guessed.
  Future<Directory> capturesRoot() async {
    final path = await _methods.invokeMethod<String>('capturesRoot');
    return Directory(path!);
  }

  Future<int> freeBytes() async =>
      await _methods.invokeMethod<int>('freeBytes') ?? 0;

  Future<bool> hasMicPermission() async =>
      await _methods.invokeMethod<bool>('hasMicPermission') ?? false;

  /// Asks for the microphone, and for notifications alongside it.
  ///
  /// Returns whether the **microphone** was granted. A refused notification
  /// permission is not reported because it must not change anything: the
  /// foreground service runs either way and only the visible indicator is lost.
  Future<bool> requestPermissions() async =>
      await _methods.invokeMethod<bool>('requestPermissions') ?? false;

  Future<void> start(String captureId, {InterruptionPolicy? policy}) =>
      _methods.invokeMethod<void>('start', {
        'captureId': captureId,
        if (policy != null) 'policy': policy.wire,
      });

  Future<String?> stop() => _methods.invokeMethod<String>('stop');

  Future<bool> pause() async =>
      await _methods.invokeMethod<bool>('pause') ?? false;

  Future<bool> resume() async =>
      await _methods.invokeMethod<bool>('resume') ?? false;

  Future<RecorderSnapshot> state() async {
    final map = await _methods.invokeMapMethod<Object?, Object?>('state');
    return map == null ? RecorderSnapshot.idle : RecorderSnapshot.fromMap(map);
  }

  /// Ticks on every state change and on each heartbeat.
  ///
  /// The UI reads RECORDING, the timer and the buffer size from here rather
  /// than from its own state, which is what lets a recreated UI re-attach to a
  /// recording already in flight.
  Stream<RecorderSnapshot> watch() => _events.receiveBroadcastStream().map(
        (e) => RecorderSnapshot.fromMap(e as Map<Object?, Object?>),
      );
}

/// Asks the service's registry whether a capture is still being written.
class PlatformCaptureOwner implements CaptureOwner {
  const PlatformCaptureOwner();

  @override
  Future<bool> isHeld(String captureId) async {
    // A PlatformException or a missing plugin propagates on purpose. "The owner
    // could not be asked" is a different answer from "nobody holds it", and
    // recovery treats it differently: the first falls back to the lease, the
    // second salvages immediately.
    final held = await _methods.invokeMethod<bool>('isHeld', {
      'captureId': captureId,
    });
    return held ?? false;
  }
}
