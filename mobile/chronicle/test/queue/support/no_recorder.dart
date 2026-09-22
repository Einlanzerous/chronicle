/// A recorder that never actually records, for any queue test that only
/// needs `capturesRoot()` answered -- every queue test writes captures
/// straight to disk rather than driving a real recording.
library;

import 'dart:io';

import 'package:chronicle/capture/capture_channel.dart';
import 'package:chronicle/capture/recovery.dart';

class NoRecorderPlatform implements CapturePlatform {
  NoRecorderPlatform(this.root);
  final Directory root;

  @override
  Future<Directory> capturesRoot() async => root;
  @override
  Future<int> freeBytes() async => 8 * 1024 * 1024 * 1024;
  @override
  Future<bool> hasMicPermission() async => true;
  @override
  Future<bool> requestPermissions() async => true;
  @override
  Future<void> start(String captureId, {InterruptionPolicy? policy}) async {}
  @override
  Future<String?> stop() async => null;
  @override
  Future<bool> pause() async => true;
  @override
  Future<bool> resume() async => true;
  @override
  Future<RecorderSnapshot> state() async => RecorderSnapshot.idle;
  @override
  Stream<RecorderSnapshot> watch() => const Stream.empty();
}

class NoOwner implements CaptureOwner {
  @override
  Future<bool> isHeld(String captureId) async => false;
}
