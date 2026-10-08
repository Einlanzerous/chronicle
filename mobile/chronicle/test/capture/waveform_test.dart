/// CHRN-113: the recording waveform flows rather than steps.
///
/// Driven through the real capture screen with a platform whose recorder
/// snapshots the test pushes by hand, so the sample cadence is the test's and
/// nothing here depends on wall time.
library;

import 'dart:async';
import 'dart:io';

import 'package:chronicle/capture/capture_channel.dart';
import 'package:chronicle/capture/capture_controller.dart';
import 'package:chronicle/features/capture/capture_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import '../queue/support/no_recorder.dart';

class _Feed extends NoRecorderPlatform {
  _Feed(super.root);
  final controller = StreamController<RecorderSnapshot>.broadcast(sync: true);
  @override
  Stream<RecorderSnapshot> watch() => controller.stream;
}

RecorderSnapshot _snap(int amplitude, int elapsedMs) => RecorderSnapshot(
      captureId: 'w',
      state: RecorderState.recording,
      elapsedMs: elapsedMs,
      byteSize: 1024,
      silenced: false,
      micOpen: true,
      hasConfig: true,
      amplitude: amplitude,
    );

void main() {
  late _Feed feed;
  var elapsed = 0;

  Future<void> boot(WidgetTester tester) async {
    feed = _Feed(Directory.systemTemp);
    elapsed = 0;
    final container = ProviderContainer(overrides: [
      capturePlatformProvider.overrideWithValue(feed),
      captureOwnerProvider.overrideWithValue(NoOwner()),
    ]);
    addTearDown(container.dispose);
    addTearDown(feed.controller.close);
    // Reading the notifier builds it, which subscribes to watch().
    container.read(captureControllerProvider);
    await tester.pumpWidget(UncontrolledProviderScope(
      container: container,
      child: const MaterialApp(home: CaptureScreen()),
    ));
  }

  /// One sample, then the 100 ms the service would wait before the next.
  Future<void> sample(WidgetTester tester, int amplitude, {int wait = 100}) async {
    elapsed += 100;
    feed.controller.add(_snap(amplitude, elapsed));
    await tester.pump();
    if (wait > 0) await tester.pump(Duration(milliseconds: wait));
  }

  Finder bar(int k) => find.byKey(ValueKey('wave-bar-$k'));
  double left(WidgetTester tester, int k) => tester.getTopLeft(bar(k)).dx;
  double height(WidgetTester tester, int k) =>
      tester.getSize(find.descendant(of: bar(k), matching: find.byType(ColoredBox)).first).height;
  Color color(WidgetTester tester, int k) => tester
      .widget<ColoredBox>(find.descendant(of: bar(k), matching: find.byType(ColoredBox)).first)
      .color;

  testWidgets('the row slides between samples instead of stepping at them', (tester) async {
    await boot(tester);
    await sample(tester, 4000, wait: 0);
    await sample(tester, 4000, wait: 0);
    final atArrival = left(tester, 1);

    await tester.pump(const Duration(milliseconds: 50));
    final half = left(tester, 1);
    await tester.pump(const Duration(milliseconds: 50));
    final full = left(tester, 1);

    final slot = tester.getSize(find.byType(ClipRect).first).width / 48;
    expect(atArrival - half, closeTo(slot / 2, slot * 0.1), reason: 'half a tick, half a bar');
    expect(atArrival - full, closeTo(slot, slot * 0.1), reason: 'a whole tick, a whole bar');
    expect((atArrival - half) % slot, isNot(0), reason: 'the offset is fractional');
  });

  testWidgets('no jump at the seam: a new sample lands where the slide ended', (tester) async {
    await boot(tester);
    await sample(tester, 4000);
    final before = left(tester, 0); // the newest bar, at the end of its slide
    feed.controller.add(_snap(4000, elapsed += 100));
    await tester.pump();
    // The previously newest bar is now index 1, in the same place.
    expect(left(tester, 1), closeTo(before, 0.5));
  });

  testWidgets('the scale does not jump when the loudest bar leaves the window', (tester) async {
    await boot(tester);
    await sample(tester, 20000);
    for (var i = 0; i < 46; i++) {
      await sample(tester, 3000);
    }
    final loud = height(tester, 46); // still in the window
    expect(loud, greaterThan(height(tester, 0)));
    final quietBefore = height(tester, 0);

    // Two more samples push the 20000 bar out of the 48-bar window.
    await sample(tester, 3000);
    await sample(tester, 3000);
    final quietAfter = height(tester, 0);
    // Under the old per-window maximum this ratio was ~6.7x; now it is the
    // decay of a couple of samples.
    expect(quietAfter / quietBefore, lessThan(1.15));
    expect(quietAfter / quietBefore, greaterThanOrEqualTo(1.0));
  });

  testWidgets('a run of zeros is still a fault, at the same threshold', (tester) async {
    await boot(tester);
    await sample(tester, 5000);
    final live = color(tester, 0);

    // 2.9 s of exact zeros: not yet a fault.
    for (var i = 0; i < 29; i++) {
      await sample(tester, 0, wait: 0);
    }
    expect(find.text('No sound is reaching the recorder.'), findsNothing);
    expect(color(tester, 0), live);

    // The 30th zero crosses 3000 ms.
    await sample(tester, 0, wait: 0);
    expect(find.text('No sound is reaching the recorder.'), findsOneWidget);
    expect(color(tester, 0), isNot(live), reason: 'the bars go grey');
    // Zeros draw the 2px minimum, not a smoothed "quiet" level.
    expect(height(tester, 0), 2.0);
    expect(height(tester, 5), 2.0);
  });
}
