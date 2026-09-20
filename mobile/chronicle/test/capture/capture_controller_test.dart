import 'dart:async';
import 'dart:io';

import 'package:chronicle/capture/capture_channel.dart';
import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/capture/recovery.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

/// A recorder that does what the test says, including refusing.
class _FakePlatform implements CapturePlatform {
  _FakePlatform(this.root);

  final Directory root;

  /// Plenty, unless a test says otherwise.
  int free = 8 * 1024 * 1024 * 1024;
  bool granted = true;
  final List<String> started = [];
  bool stopped = false;

  RecorderSnapshot snapshot = RecorderSnapshot.idle;
  final _events = StreamController<RecorderSnapshot>.broadcast();

  void emit(RecorderSnapshot s) {
    snapshot = s;
    _events.add(s);
  }

  @override
  Future<Directory> capturesRoot() async => root;

  @override
  Future<int> freeBytes() async => free;

  @override
  Future<bool> hasMicPermission() async => granted;

  @override
  Future<bool> requestPermissions() async => granted;

  @override
  Future<void> start(String captureId, {InterruptionPolicy? policy}) async {
    started.add(captureId);
  }

  @override
  Future<String?> stop() async {
    stopped = true;
    emit(RecorderSnapshot.idle);
    return null;
  }

  @override
  Future<bool> pause() async => true;

  @override
  Future<bool> resume() async => true;

  @override
  Future<RecorderSnapshot> state() async => snapshot;

  @override
  Stream<RecorderSnapshot> watch() => _events.stream;
}

class _NoOwner implements CaptureOwner {
  @override
  Future<bool> isHeld(String captureId) async => false;
}

RecorderSnapshot _recording({required int elapsedMs}) => RecorderSnapshot(
      captureId: 'c',
      state: RecorderState.recording,
      elapsedMs: elapsedMs,
      byteSize: 1024,
      silenced: false,
      micOpen: true,
      hasConfig: true,
      amplitude: 500,
    );

void main() {
  late Directory root;
  late _FakePlatform platform;
  late ProviderContainer container;

  setUp(() async {
    root = await Directory.systemTemp.createTemp('chrn60ctl');
    platform = _FakePlatform(root);
    container = ProviderContainer(overrides: [
      capturePlatformProvider.overrideWithValue(platform),
      captureOwnerProvider.overrideWithValue(_NoOwner()),
    ]);
  });

  tearDown(() async {
    // Dispose first, then let any in-flight recovery settle, THEN delete. The
    // controller kicks recovery off unawaited at build, and deleting the
    // directory out from under it made one test fail inside another.
    container.dispose();
    await Future<void>.delayed(Duration.zero);
    if (await root.exists()) await root.delete(recursive: true);
  });

  CaptureController ctl() => container.read(captureControllerProvider.notifier);
  CaptureUiState st() => container.read(captureControllerProvider);

  group('the free-space floor refuses with a number somebody can act on', () {
    test('below the floor, nothing starts and the message names the bytes free',
        () async {
      // Deliberately NOT exercised by filling a real phone's storage: bringing a
      // daily-driver device under a 272 MB floor means writing tens of
      // gigabytes and risking the system, which is not a reasonable thing to do
      // to somebody's phone to prove a branch this test proves exactly.
      platform.free = 100 * 1024 * 1024; // 100 MB
      await ctl().toggle();

      expect(platform.started, isEmpty, reason: 'no recorder may be started');
      expect(st().refusal, isNotNull);
      expect(st().refusal, contains('100 MB'));
      expect(st().refusal, contains('272 MB'));
    });

    test('above the floor it starts', () async {
      platform.free = 4 * 1024 * 1024 * 1024;
      await ctl().toggle();
      expect(platform.started, hasLength(1));
      expect(st().refusal, isNull);
    });

    test('a refused microphone is said plainly and starts nothing', () async {
      platform.granted = false;
      await ctl().toggle();
      expect(platform.started, isEmpty);
      expect(st().refusal, contains('microphone'));
    });
  });

  group('the capture record is written before the first audio byte', () {
    test('meta.json exists with its key the moment start is called', () async {
      await ctl().toggle();
      final id = platform.started.single;
      final record = await CaptureDir(root, id).readMeta();
      expect(record, isNotNull);
      expect(record!.idempotencyKey, contains(id));
      expect(record.idempotencyKey.length, greaterThanOrEqualTo(16));
      expect(record.state.name, 'recording');
      expect(record.retention, isNull);
    });
  });

  group('hold, release and cancel', () {
    test('a release BEFORE the recorder reports in latches rather than vanishing',
        () async {
      // The device-found defect: starting is asynchronous, so a quick tap's
      // release lands while isRecording is still false. Gating on it dropped
      // the release entirely for exactly the shortest gestures.
      await ctl().beginHold();
      await ctl().endHold();
      expect(st().latched, isTrue);
      expect(platform.stopped, isFalse, reason: 'a brush must not end a memo');
    });

    test('a release after real audio stops the recording', () async {
      await ctl().beginHold();
      platform.emit(_recording(elapsedMs: 2000));
      await Future<void>.delayed(Duration.zero);
      await ctl().endHold();
      expect(platform.stopped, isTrue);
    });

    test('a release under the threshold latches instead of stopping', () async {
      await ctl().beginHold();
      platform.emit(_recording(elapsedMs: 120));
      await Future<void>.delayed(Duration.zero);
      await ctl().endHold();
      expect(st().latched, isTrue);
      expect(platform.stopped, isFalse);
    });

    test('a pointer CANCEL keeps recording and latches', () async {
      // A cancel is not a release: the system cancels a pointer for the
      // notification shade, palm rejection, a system gesture. Treating those as
      // "stop" would end a memo mid-thought.
      await ctl().beginHold();
      platform.emit(_recording(elapsedMs: 3000));
      await Future<void>.delayed(Duration.zero);
      ctl().cancelHold();
      expect(st().latched, isTrue);
      expect(platform.stopped, isFalse);
    });
  });
}
