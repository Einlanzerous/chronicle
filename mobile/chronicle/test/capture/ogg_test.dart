import 'dart:typed_data';

import 'package:chronicle/capture/ogg.dart';
import 'package:flutter_test/flutter_test.dart';

import 'ogg_fixtures.dart';

void main() {
  group('the CRC is the one Ogg specifies', () {
    test('known vector: the polynomial is 0x04c11db7, unreflected', () {
      // The CRC of the ASCII bytes "123456789" under OGG's parameters: poly
      // 0x04c11db7, init 0, no reflection, no final xor.
      //
      // Note which value this is NOT. The widely quoted 0x0376E6E7 is
      // CRC-32/MPEG-2, which shares the polynomial but initialises to
      // 0xFFFFFFFF -- and reaching for it here is exactly the mistake this test
      // exists to catch, because an init-0xFFFFFFFF implementation fails every
      // real Ogg page. The value below was cross-checked against a third
      // implementation outside this codebase before being pinned.
      expect(oggCrc('123456789'.codeUnits), 0x89A1897F);
    });

    test('a page whose CRC does not match is not kept', () {
      final buf = BytesBuilder()
        ..add(oggPage(serial: 1, sequence: 0, granule: 0, body: opusHead(), headerType: 0x02))
        ..add(oggPage(serial: 1, sequence: 1, granule: 0, body: opusTags()))
        ..add(oggPage(serial: 1, sequence: 2, granule: 1272, body: List.filled(40, 1)))
        ..add(oggPage(
          serial: 1,
          sequence: 3,
          granule: 2232,
          body: List.filled(40, 2),
          corruptCrc: true,
        ));
      final bytes = buf.toBytes();
      final scan = scanOgg(bytes);

      // Three good pages kept, the corrupt fourth dropped — and critically the
      // trim lands BEFORE it rather than at EOF.
      expect(scan.pagesOk, 3);
      expect(scan.trimOffset, lessThan(bytes.length));
      expect(scan.endsAtEof, isFalse);
      expect(scan.lastGranule, 1272);
    });

    test('a length-valid page with a stale body is caught, which arithmetic alone would miss', () {
      // This is the power-loss shape: the page's length reached the disk, its
      // body did not. The segment table still adds up, so the server's
      // length-only walk would accept it.
      final bytes = oggStream(audioPages: 4);
      final pages = readPages(bytes);
      final victim = pages[4];
      bytes.fillRange(victim.bodyOffset, victim.end, 0x00);

      final scan = scanOgg(bytes);
      expect(scan.pagesOk, 4, reason: 'the zero-filled page must not be kept');
      expect(scan.trimOffset, victim.offset);
    });
  });

  group('the trim finds where a torn recording really ends', () {
    test('a whole file trims to itself, byte for byte', () {
      final bytes = oggStream(audioPages: 6);
      final scan = scanOgg(bytes);
      expect(scan.endsAtEof, isTrue);
      expect(scan.trimOffset, bytes.length);
      expect(scan.audioPages, 6);
    });

    test('truncation at every page boundary keeps exactly the pages before it', () {
      final whole = oggStream(audioPages: 8);
      for (final page in readPages(whole)) {
        final cut = Uint8List.sublistView(whole, 0, page.offset);
        if (cut.isEmpty) continue;
        final scan = scanOgg(cut);
        expect(scan.trimOffset, page.offset,
            reason: 'a cut on a boundary is already whole; nothing to discard');
        expect(scan.endsAtEof, isTrue);
      }
    });

    test('truncation mid-header discards the partial page', () {
      final whole = oggStream(audioPages: 8);
      final pages = readPages(whole);
      final last = pages.last;
      for (final into in [1, 4, 13, 26]) {
        final cut = Uint8List.sublistView(whole, 0, last.offset + into);
        final scan = scanOgg(cut);
        expect(scan.trimOffset, last.offset,
            reason: 'a header cut $into bytes in cannot be a complete page');
        expect(scan.endsAtEof, isFalse);
      }
    });

    test('truncation mid-body discards the partial page', () {
      final whole = oggStream(audioPages: 8);
      final pages = readPages(whole);
      final last = pages.last;
      final cut = Uint8List.sublistView(whole, 0, last.bodyOffset + 10);
      final scan = scanOgg(cut);
      expect(scan.trimOffset, last.offset);
      expect(scan.endsAtEof, isFalse);
    });

    test('truncation one byte before the end is still one page short', () {
      final whole = oggStream(audioPages: 8);
      final pages = readPages(whole);
      final cut = Uint8List.sublistView(whole, 0, whole.length - 1);
      final scan = scanOgg(cut);
      expect(scan.trimOffset, pages.last.offset);
      expect(scan.pagesOk, pages.length - 1);
    });
  });

  group('the duration matches the arithmetic the server uses', () {
    test('(granule - pre_skip) / 48000, rounded', () {
      // 25 audio pages of one 20 ms frame each = 500 ms.
      final scan = scanOgg(oggStream(audioPages: 25));
      expect(scan.preSkip, 312);
      expect(scan.lastGranule, 312 + 25 * 960);
      expect(scan.durationMs, 500);
    });

    test('a trimmed stream reports the shorter duration, not the original', () {
      final whole = oggStream(audioPages: 25);
      final pages = readPages(whole);
      final cut = Uint8List.sublistView(whole, 0, pages[12].end);
      final scan = scanOgg(cut);
      // Pages 0 and 1 are the headers, so page index 12 is the 11th audio page.
      expect(scan.durationMs, 220);
      expect(scan.audioPages, 11);
    });
  });

  group('a capture that recovered no audio is empty, and that is a fact to keep', () {
    test('headers only means zero audio pages', () {
      final out = BytesBuilder()
        ..add(oggPage(serial: 7, sequence: 0, granule: 0, body: opusHead(), headerType: 0x02))
        ..add(oggPage(serial: 7, sequence: 1, granule: 0, body: opusTags()));
      final scan = scanOgg(out.toBytes());
      expect(scan.isEmpty, isTrue);
      expect(scan.audioPages, 0);
      expect(scan.durationMs, anyOf(isNull, 0));
    });

    test('a file with nothing readable at all is empty rather than an error', () {
      final scan = scanOgg(Uint8List.fromList([0, 1, 2, 3, 4]));
      expect(scan.isEmpty, isTrue);
      expect(scan.pagesOk, 0);
      expect(scan.trimOffset, 0);
    });
  });
}
