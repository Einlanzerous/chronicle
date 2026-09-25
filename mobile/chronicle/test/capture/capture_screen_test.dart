/// CHRN-119: the capture screen's own half of dismissing an `empty`
/// capture. `capture_controller_test.dart` proves the mechanics (hides,
/// deletes nothing, refuses anything but `empty`); this proves the screen
/// actually wires a tap to them.
///
/// Real filesystem calls go through `tester.runAsync` -- see
/// `queue_screen_test.dart`'s own header note for why.
library;

import 'dart:io';

import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/capture/capture_record.dart';
import 'package:chronicle/features/capture/capture_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import '../queue/support/no_recorder.dart';

/// Polls rather than sleeping a fixed amount: `dismiss()` completes on the
/// real event loop, fired-and-forgotten from a tap the widget never awaits,
/// so how long it takes depends on how loaded the machine running the test
/// is -- a fixed delay that is generous alone is exactly what flakes under
/// a full, parallel test-suite run.
Future<void> _pumpUntil(WidgetTester tester, bool Function() condition) async {
  for (var i = 0; i < 80; i++) {
    if (condition()) return;
    await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 25)));
    await tester.pump();
  }
  expect(condition(), isTrue, reason: 'condition not met within 2s of polling');
}

void main() {
  late Directory root;

  tearDown(() async {
    if (await root.exists()) await root.delete(recursive: true);
  });

  testWidgets('DISMISS on an EMPTY row hides it and deletes nothing', (tester) async {
    late ProviderContainer container;
    await tester.runAsync(() async {
      root = await Directory.systemTemp.createTemp('chrn119-screen');
      await CaptureDir(root, 'e').writeMeta(CaptureRecord(
        captureId: 'e',
        idempotencyKey: 'chr-cap-e',
        startedAt: DateTime(2026, 1, 1),
        state: CaptureState.empty,
      ));
    });
    container = ProviderContainer(overrides: [
      capturePlatformProvider.overrideWithValue(NoRecorderPlatform(root)),
      captureOwnerProvider.overrideWithValue(NoOwner()),
    ]);
    addTearDown(container.dispose);

    await tester.pumpWidget(
      UncontrolledProviderScope(
        container: container,
        child: const MaterialApp(home: CaptureScreen()),
      ),
    );
    await tester.runAsync(
      () => container.read(captureControllerProvider.notifier).refresh(),
    );
    await tester.pump();

    expect(find.text('EMPTY'), findsOneWidget);
    expect(find.text('DISMISS'), findsOneWidget);

    await tester.runAsync(() => tester.tap(find.text('DISMISS')));
    await tester.pump();
    await _pumpUntil(tester, () => find.text('EMPTY').evaluate().isEmpty);

    expect(find.text('EMPTY'), findsNothing);
    expect(find.text('Nothing captured yet.'), findsOneWidget);

    final capture = CaptureDir(root, 'e');
    await tester.runAsync(() async {
      expect(await capture.isDismissed(), isTrue);
      expect(await capture.dir.exists(), isTrue);
      expect(await capture.meta.exists(), isTrue);
    });
  });
}
