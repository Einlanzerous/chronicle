/// The app's [ThemeData], built from `tokens.dart` and from nothing else.
///
/// There are no hex literals in this file or anywhere outside `tokens.dart`,
/// which is the rule `scripts/check-tokens.sh` enforces on the web client for
/// the same reason: a colour written inline is a colour that will not change
/// when the canvas does.
library;

import 'package:flutter/material.dart';

import 'tokens.dart';

/// Letter-spacing in logical pixels for a mono micro-label at [size].
///
/// CSS tracking is in `em` (relative to font size); Flutter's `letterSpacing`
/// is absolute. Porting `0.16em` as `0.16` would be a 6x under-track at 9.5px
/// and the loud mono labels would read as ordinary text.
double trackWide(double size) => trackWideEm * size;

/// As [trackWide], for the quieter meta captions.
double trackMid(double size) => trackMidEm * size;

ThemeData chronicleTheme() {
  final scheme = ColorScheme.fromSeed(
    seedColor: chSignal,
    brightness: Brightness.dark,
  ).copyWith(
    surface: chBase,
    onSurface: chText,
    surfaceContainerHighest: chRaised,
    outline: chLine,
    primary: chSignal,
    onPrimary: chBase,
    error: refSwitchyard,
  );

  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: chBase,
    fontFamily: fontSans,
    textTheme: const TextTheme(
      bodyLarge: TextStyle(fontSize: sizeBase, color: chText),
      bodyMedium: TextStyle(fontSize: sizeBody, color: chText),
      bodySmall: TextStyle(fontSize: sizeXs, color: chText2),
      titleMedium: TextStyle(fontSize: sizeMd, color: chText),
      labelSmall: TextStyle(fontSize: sizeSm, color: chText2),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: chRaised,
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(space1),
        borderSide: const BorderSide(color: chLine),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(space1),
        borderSide: const BorderSide(color: chLine),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        // The epic's floor: 44 px minimum tap targets, per the canvas.
        minimumSize: const Size(minTapTarget, minTapTarget),
        backgroundColor: chSignal,
        foregroundColor: chBase,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(space1),
        ),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        minimumSize: const Size(minTapTarget, minTapTarget),
        foregroundColor: chText2,
      ),
    ),
  );
}

/// The mono micro-label style the canvas uses for section headers and stamps --
/// `TIER 2 · AUTHORED`, `READ ONLY`. Uppercase, wide-tracked, quiet.
TextStyle microLabel({Color color = chTextMeta, double size = sizeXxs}) =>
    TextStyle(
      fontFamily: fontMono,
      fontSize: size,
      letterSpacing: trackWide(size),
      color: color,
      fontWeight: FontWeight.w500,
    );

/// The mono caption style for exact values that are not labels -- a host name,
/// a timestamp, a duration.
TextStyle monoMeta({Color color = chText2, double size = sizeXs}) => TextStyle(
      fontFamily: fontMono,
      fontSize: size,
      letterSpacing: trackMid(size),
      color: color,
    );
