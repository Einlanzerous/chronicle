/// Reading Ogg pages, and deciding where a torn recording actually ends.
///
/// **This file is why the container is Ogg.** MPEG-4 writes its index at
/// `stop()`, so a recording killed before stop is a file with no index — not
/// playable, not probeable, not repairable on a phone. Ogg is a chain of
/// self-describing pages, so a file cut anywhere is still a valid stream up to
/// its last complete page. Finding that page is all a salvage is.
///
/// ## Why a CRC check and not just arithmetic
///
/// The server's probe (`internal/audio/probe.go`) locates a stream's end by
/// summing the segment table and requiring the result to land exactly on EOF.
/// It never reads a page CRC — `probe_test.go` builds its fixtures with the
/// comment `// CRC, not checked`. That is the right trade *there*, where the
/// file arrived whole over a hash-checked transfer.
///
/// It is the wrong trade here. This code runs on the one path where something
/// has already gone wrong, and the failure it has to catch is a page whose
/// **length** reached the disk before its **body** did — which is exactly what
/// a power loss leaves behind, and exactly what a length-only check waves
/// through. Every Ogg page carries a CRC-32 in its header; checking it costs a
/// table and one pass, and turns "complete page" from a coincidence into a
/// checked fact.
///
/// ## What the server does with the result
///
/// A trimmed file ends on a page boundary, so the probe's EOF arithmetic
/// succeeds and the memo arrives described: duration, codec, sample rate. Hand
/// over the untrimmed file instead and the probe reads the torn tail as *"a
/// chain or has data appended"*, declines, and the memo lands with three NULL
/// columns — for want of arithmetic that fits in this file.
///
/// The EOS flag being unset on a trimmed stream costs nothing: the probe checks
/// the Ogg version, the stream serial, EOF alignment and a non-negative
/// granule, and never looks at EOS.
library;

import 'dart:typed_data';

/// Fixed part of an Ogg page header, before the segment table.
const int oggHeaderBytes = 27;

/// `OggS`, the page capture pattern.
const List<int> _magic = [0x4F, 0x67, 0x67, 0x53];

/// Opus counts granule positions at a fixed 48 kHz whatever the source rate,
/// which is what makes a duration arithmetic rather than a decode.
const int _opusGranuleRate = 48000;

/// Ogg's CRC-32: polynomial 0x04c11db7, initial value 0, **no** input or output
/// reflection and no final xor. It is not the zlib/PNG CRC-32, and using that
/// one by mistake fails every page, which at least fails loudly.
final Uint32List _crcTable = _buildCrcTable();

Uint32List _buildCrcTable() {
  final table = Uint32List(256);
  for (var i = 0; i < 256; i++) {
    var r = i << 24;
    for (var j = 0; j < 8; j++) {
      r = (r & 0x80000000) != 0 ? ((r << 1) ^ 0x04C11DB7) : (r << 1);
      r &= 0xFFFFFFFF;
    }
    table[i] = r;
  }
  return table;
}

/// One page, as found in the buffer.
class OggPage {
  const OggPage({
    required this.offset,
    required this.end,
    required this.granule,
    required this.serial,
    required this.sequence,
    required this.headerType,
    required this.crcOk,
    required this.bodyOffset,
    required this.bodyLength,
  });

  final int offset;
  final int end;
  final int granule;
  final int serial;
  final int sequence;
  final int headerType;
  final bool crcOk;
  final int bodyOffset;
  final int bodyLength;

  /// A continuation, header or EOS page carries no new audio of its own that a
  /// duration can be read from; `-1` means no packet completes on this page.
  bool get carriesGranule => granule >= 0;
}

/// What a scan found, and what to do about it.
class OggScan {
  const OggScan({
    required this.trimOffset,
    required this.pagesOk,
    required this.audioPages,
    required this.serial,
    required this.preSkip,
    required this.lastGranule,
    required this.durationMs,
    required this.endsAtEof,
  });

  /// Where the last CRC-valid, complete page ends. The length to keep.
  final int trimOffset;

  final int pagesOk;

  /// Pages past `OpusHead` and `OpusTags`.
  ///
  /// **Zero is the number that matters.** A capture with no audio pages is one
  /// whose audio never left the encoder, and it is marked `empty` and kept —
  /// never cleaned up. The remnant is the only visible trace that a memo was
  /// ever attempted, and deleting it would make the worst loss this system can
  /// suffer completely silent.
  final int audioPages;

  final int? serial;

  /// Encoder delay, from `OpusHead`. Subtracting it is the whole difference
  /// between this and `ffprobe`, which divides the granule and stops.
  final int? preSkip;

  final int? lastGranule;
  final int? durationMs;

  /// True when nothing follows the last good page — i.e. the file was already
  /// whole and a trim would copy it byte for byte.
  final bool endsAtEof;

  bool get isEmpty => audioPages == 0;
}

/// Walks pages from the start, stopping at the first that is truncated or fails
/// its CRC.
///
/// Forwards rather than backwards, which is the opposite of the server's tail
/// scan and is deliberate: the server has a whole file and only wants its last
/// granule, while this has a *suspect* file and has to know that every page it
/// keeps is sound. A backwards scan that found a plausible header in the middle
/// of compressed payload would keep a prefix containing a corrupt page.
List<OggPage> readPages(Uint8List buf) {
  final pages = <OggPage>[];
  final view = ByteData.sublistView(buf);
  var i = 0;

  while (i + oggHeaderBytes <= buf.length) {
    if (buf[i] != _magic[0] ||
        buf[i + 1] != _magic[1] ||
        buf[i + 2] != _magic[2] ||
        buf[i + 3] != _magic[3]) {
      break;
    }
    // Version 0 is the only Ogg version defined. Anything else is not a page
    // header, whatever the four magic bytes suggest.
    if (buf[i + 4] != 0) break;

    final segments = buf[i + 26];
    final tableEnd = i + oggHeaderBytes + segments;
    if (tableEnd > buf.length) break;

    var bodyLength = 0;
    for (var s = 0; s < segments; s++) {
      bodyLength += buf[tableEnd - segments + s];
    }
    final end = tableEnd + bodyLength;
    if (end > buf.length) break; // truncated mid-body: not a complete page

    final page = OggPage(
      offset: i,
      end: end,
      granule: view.getInt64(i + 6, Endian.little),
      serial: view.getUint32(i + 14, Endian.little),
      sequence: view.getUint32(i + 18, Endian.little),
      headerType: buf[i + 5],
      crcOk: _pageCrcOk(buf, i, end, view.getUint32(i + 22, Endian.little)),
      bodyOffset: tableEnd,
      bodyLength: bodyLength,
    );
    pages.add(page);
    if (!page.crcOk) break;
    i = end;
  }
  return pages;
}

bool _pageCrcOk(Uint8List buf, int start, int end, int stored) {
  var r = 0;
  for (var i = start; i < end; i++) {
    // The CRC is computed over the whole page with its own CRC field zeroed.
    final b = (i >= start + 22 && i < start + 26) ? 0 : buf[i];
    r = ((r << 8) & 0xFFFFFFFF) ^ _crcTable[((r >> 24) & 0xFF) ^ b];
  }
  return r == stored;
}

/// Scans [buf] and reports where a salvage should cut it.
OggScan scanOgg(Uint8List buf) {
  final pages = readPages(buf);
  final good = pages.where((p) => p.crcOk).toList(growable: false);

  if (good.isEmpty) {
    return const OggScan(
      trimOffset: 0,
      pagesOk: 0,
      audioPages: 0,
      serial: null,
      preSkip: null,
      lastGranule: null,
      durationMs: null,
      endsAtEof: false,
    );
  }

  final head = good.first;
  int? preSkip;
  if (head.bodyLength >= 12 &&
      _matches(buf, head.bodyOffset, 'OpusHead')) {
    preSkip = ByteData.sublistView(buf).getUint16(head.bodyOffset + 10, Endian.little);
  }

  // `OpusHead` and `OpusTags` are mandatory and carry no audio. Anything past
  // them does.
  final audioPages = good.length > 2 ? good.length - 2 : 0;

  int? lastGranule;
  for (final p in good.reversed) {
    if (p.carriesGranule && p.granule > 0) {
      lastGranule = p.granule;
      break;
    }
  }

  int? durationMs;
  if (lastGranule != null && preSkip != null) {
    final samples = lastGranule - preSkip;
    if (samples > 0) {
      // Rounded, matching the server's arithmetic so the two agree to the
      // millisecond rather than to within a frame.
      durationMs = (samples * 1000 + _opusGranuleRate ~/ 2) ~/ _opusGranuleRate;
    } else {
      durationMs = 0;
    }
  }

  final trimOffset = good.last.end;
  return OggScan(
    trimOffset: trimOffset,
    pagesOk: good.length,
    audioPages: audioPages,
    serial: head.serial,
    preSkip: preSkip,
    lastGranule: lastGranule,
    durationMs: durationMs,
    endsAtEof: trimOffset == buf.length,
  );
}

bool _matches(Uint8List buf, int offset, String magic) {
  if (offset + magic.length > buf.length) return false;
  for (var i = 0; i < magic.length; i++) {
    if (buf[offset + i] != magic.codeUnitAt(i)) return false;
  }
  return true;
}
