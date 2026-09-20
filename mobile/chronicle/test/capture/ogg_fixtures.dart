/// Ogg fixtures, built byte by byte so truncation offsets are exact.
///
/// Shared by the trim tests and the recovery tests: both need real streams, and
/// a fixture that drifted between them would make one suite lie about the
/// other.
library;

import 'dart:typed_data';

import 'package:chronicle/capture/ogg.dart';

/// Ogg's CRC-32, written a second time on purpose.
///
/// The fixtures below are built with this and checked with the implementation's
/// own table. Sharing one function would make a wrong polynomial agree with
/// itself, which is the one bug a CRC test exists to catch — so this is a
/// deliberate duplicate, and the `known vector` test pins it to a value neither
/// copy produced.
int oggCrc(List<int> data) {
  var r = 0;
  for (final b in data) {
    var x = ((r >> 24) & 0xFF) ^ b;
    var t = x << 24;
    for (var i = 0; i < 8; i++) {
      t = (t & 0x80000000) != 0 ? ((t << 1) ^ 0x04C11DB7) : (t << 1);
      t &= 0xFFFFFFFF;
    }
    r = ((r << 8) & 0xFFFFFFFF) ^ t;
  }
  return r;
}

/// Builds one Ogg page. [bodies] are the packets; each becomes lacing values.
Uint8List oggPage({
  required int serial,
  required int sequence,
  required int granule,
  required List<int> body,
  int headerType = 0,
  bool corruptCrc = false,
}) {
  final segments = <int>[];
  var remaining = body.length;
  while (remaining >= 255) {
    segments.add(255);
    remaining -= 255;
  }
  segments.add(remaining);

  final page = BytesBuilder();
  final header = Uint8List(oggHeaderBytes);
  final view = ByteData.sublistView(header);
  header[0] = 0x4F; // O
  header[1] = 0x67; // g
  header[2] = 0x67; // g
  header[3] = 0x53; // S
  header[4] = 0; // version
  header[5] = headerType;
  view.setInt64(6, granule, Endian.little);
  view.setUint32(14, serial, Endian.little);
  view.setUint32(18, sequence, Endian.little);
  view.setUint32(22, 0, Endian.little); // CRC, zeroed for the computation
  header[26] = segments.length;

  page.add(header);
  page.add(Uint8List.fromList(segments));
  page.add(Uint8List.fromList(body));

  final bytes = page.toBytes();
  final crc = corruptCrc ? oggCrc(bytes) ^ 0xFFFF : oggCrc(bytes);
  ByteData.sublistView(bytes).setUint32(22, crc, Endian.little);
  return bytes;
}

/// `OpusHead` with a 312-sample pre-skip, which is what libopus writes and what
/// CHRN-21 measured on every one of its seven fixtures.
List<int> opusHead({int preSkip = 312}) {
  final b = Uint8List(19);
  b.setRange(0, 8, 'OpusHead'.codeUnits);
  b[8] = 1; // version
  b[9] = 1; // channels
  ByteData.sublistView(b).setUint16(10, preSkip, Endian.little);
  ByteData.sublistView(b).setUint32(12, 48000, Endian.little);
  return b;
}

List<int> opusTags() => [...'OpusTags'.codeUnits, 0, 0, 0, 0, 0, 0, 0, 0];

/// A whole stream: header, tags, and [audioPages] pages of 20 ms each.
Uint8List oggStream({int audioPages = 5, int serial = 0xC0FFEE}) {
  final out = BytesBuilder();
  out.add(oggPage(serial: serial, sequence: 0, granule: 0, body: opusHead(), headerType: 0x02));
  out.add(oggPage(serial: serial, sequence: 1, granule: 0, body: opusTags()));
  for (var i = 0; i < audioPages; i++) {
    out.add(oggPage(
      serial: serial,
      sequence: 2 + i,
      // 960 samples at 48 kHz is one 20 ms Opus frame. The first audio page
      // lands at pre-skip + one frame.
      granule: 312 + (i + 1) * 960,
      body: List<int>.filled(40, 0x5A),
    ));
  }
  return out.toBytes();
}

