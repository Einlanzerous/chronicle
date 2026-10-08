import 'dart:io';
import 'dart:typed_data';

import 'package:chronicle/theme/tokens.dart';
import 'package:flutter/painting.dart';
import 'package:flutter_test/flutter_test.dart';

/// CHRN-117. The launcher icon is native XML, which cannot import
/// `lib/theme/tokens.dart`, so its vellum is a second literal. This is the
/// guard CHRN-115's `favicon.test.ts` is on the web: read the resource, assert
/// it is the token, fail on a third colour.
void main() {
  final res = Directory('android/app/src/main/res');

  String read(String path) => File('${res.path}/$path').readAsStringSync();

  // Comments carry prose, and prose can mention a colour.
  String stripComments(String xml) =>
      xml.replaceAll(RegExp(r'<!--[\s\S]*?-->'), '');

  Set<int> coloursIn(String xml) => RegExp(r'#([0-9a-fA-F]{6,8})\b')
      .allMatches(stripComments(xml))
      .map((m) {
    final hex = m.group(1)!;
    return int.parse(hex.length == 6 ? 'FF$hex' : hex, radix: 16);
  }).toSet();

  // Ink is Placard's chronicle-mark-light.svg, not a Chronicle token.
  const ink = 0xFF14151A;

  test('the adaptive background is vellum, the chSignal token', () {
    final xml = stripComments(read('values/ic_launcher_background.xml'));
    final found = RegExp(
      r'<color name="ic_launcher_background">(#[0-9a-fA-F]{6})</color>',
    ).firstMatch(xml);
    expect(found, isNotNull, reason: 'no ic_launcher_background colour');

    final native = Color(int.parse('FF${found!.group(1)!.substring(1)}', radix: 16));
    expect(native, chSignal);
  });

  test('the glyph is ink and the icon uses no colour beyond vellum and ink', () {
    final used = <int>{
      ...coloursIn(read('values/ic_launcher_background.xml')),
      ...coloursIn(read('drawable/ic_launcher_foreground.xml')),
      ...coloursIn(read('mipmap-anydpi-v26/ic_launcher.xml')),
      ...coloursIn(read('mipmap-anydpi-v26/ic_launcher_round.xml')),
    };
    expect(used, {chSignal.toARGB32(), ink});
  });

  test('both adaptive definitions carry background, foreground and monochrome', () {
    for (final name in ['ic_launcher', 'ic_launcher_round']) {
      final xml = stripComments(read('mipmap-anydpi-v26/$name.xml'));
      expect(xml, contains('<background android:drawable="@color/ic_launcher_background"/>'));
      expect(xml, contains('<foreground android:drawable="@drawable/ic_launcher_foreground"/>'));
      expect(xml, contains('<monochrome android:drawable="@drawable/ic_launcher_foreground"/>'));
    }
  });

  test('the manifest names both the icon and the round icon', () {
    final manifest = File('android/app/src/main/AndroidManifest.xml').readAsStringSync();
    expect(manifest, contains('android:icon="@mipmap/ic_launcher"'));
    expect(manifest, contains('android:roundIcon="@mipmap/ic_launcher_round"'));
  });

  test('every legacy mipmap is a PNG of its density size', () {
    const sizes = {'mdpi': 48, 'hdpi': 72, 'xhdpi': 96, 'xxhdpi': 144, 'xxxhdpi': 192};
    sizes.forEach((density, px) {
      final b = File('${res.path}/mipmap-$density/ic_launcher.png').readAsBytesSync();
      final data = ByteData.sublistView(b);
      expect(data.getUint32(16), px, reason: '$density width');
      expect(data.getUint32(20), px, reason: '$density height');
    });
  });
}
