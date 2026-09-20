import 'dart:io';
import 'dart:typed_data';

import 'package:chronicle/capture/ogg.dart';
import 'package:flutter_test/flutter_test.dart';

/// The one test here whose input is a real recording rather than bytes this
/// suite assembled.
///
/// Everything in `ogg_test.dart` is built page by page, which is the right
/// default — it makes the shape under test explicit. But a hand-built stream
/// can only contain what we already believed about Ogg. These fixtures came off
/// an Android phone through `MediaRecorder`'s Ogg/Opus writer, so they carry
/// that writer's real page geometry rather than this suite's idea of it.
///
/// **The fixtures deliberately live in the Go package's `testdata`, and are read
/// across the repository rather than copied.** That is the entire point: the
/// same bytes are checked from both sides. This side asserts that trimming the
/// torn file reproduces the trimmed file exactly; `internal/audio/probe_device_test.go`
/// asserts that what this side produced is something the server can describe.
/// A second copy under `mobile/` would let the two drift and quietly turn a
/// cross-language pin into two unrelated tests.
///
/// The audio is a generated 440/660/880 Hz tone the phone played through its own
/// speaker into its own microphone — the repository is public, so the fixture is
/// device-written and is nobody's voice.
const _testdata = '../../internal/audio/testdata';

/// What the trim reported when the fixtures were made, and what the Go side
/// pins independently.
const _trimOffset = 20076;
const _durationMs = 4514;
const _preSkip = 312;

Uint8List _read(String name) =>
    Uint8List.fromList(File('$_testdata/$name').readAsBytesSync());

void main() {
  group('the device fixtures pin this trim to the server probe', () {
    test('trimming the torn recording reproduces the trimmed fixture exactly',
        () {
      final torn = _read('chrn60_torn.opus');
      final expected = _read('chrn60_trimmed.opus');

      final scan = scanOgg(torn);
      final produced = Uint8List.sublistView(torn, 0, scan.trimOffset);

      expect(produced, equals(expected),
          reason: 'if this fails, this trim and the committed fixture no longer '
              'agree — and the Go side is still asserting against the fixture, '
              'so the two implementations have drifted');
    });

    test('the torn file is genuinely torn, not merely short', () {
      final torn = _read('chrn60_torn.opus');
      final scan = scanOgg(torn);

      // A partial page at the end is the shape an interrupted write leaves, and
      // it is the shape the server refuses to describe: its last complete page
      // does not end at EOF.
      expect(scan.endsAtEof, isFalse);
      expect(torn.length - scan.trimOffset, greaterThan(0));
      expect(scan.trimOffset, _trimOffset);
    });

    test('the arithmetic agrees with the numbers the Go side pins', () {
      final scan = scanOgg(_read('chrn60_trimmed.opus'));
      expect(scan.preSkip, _preSkip);
      expect(scan.durationMs, _durationMs);
      expect(scan.endsAtEof, isTrue,
          reason: 'the trimmed fixture must be a whole stream, or the server '
              'declines it a duration');
    });

    test('every page of a real device recording passes its CRC', () {
      // Worth asserting on real bytes rather than built ones: the CRC check is
      // the client's own addition -- the server never reads a page CRC -- so
      // this is the only place a wrong polynomial would show up against a file
      // the polynomial did not produce.
      final pages = readPages(_read('chrn60_trimmed.opus'));
      expect(pages, isNotEmpty);
      expect(pages.every((p) => p.crcOk), isTrue);
      expect(pages.last.end, _trimOffset);
    });
  });
}
