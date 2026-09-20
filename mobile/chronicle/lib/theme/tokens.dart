/// Design tokens, ported from `web/src/styles/tokens.css` (CHRN-54/CHRN-58).
///
/// **This is a port, not a second source.** The canvas
/// (`Chronicle.dc.html`, pass 01 · tokens) is where the values come from, the
/// web client read them off it first, and this file follows that reading so the
/// two clients cannot drift into two palettes. Changing a colour here without
/// changing it there is a bug, and the estate's third Flutter app does the same
/// thing: Lyceum's README calls its own token port out as the pattern.
///
/// Every colour is named by ROLE, never by hue. A widget reaches for
/// [refSwitchyard] because it is drawing a Switchyard reference, never because
/// coral looked right for something else.
library;

import 'package:flutter/painting.dart';

/// The one colour that belongs to Chronicle itself rather than to something it
/// links out to -- vellum.
const chSignal = Color(0xFFE0D5BE);

/// RESERVED, estate-wide. These two mean Switchyard and Amber and nothing else,
/// in this app or any other app in the estate: CLAUDE.md invariant 2 is
/// *"coral is Switchyard, gold is Amber, anywhere either resolves"*. They belong
/// on a reference card's upstream state and nowhere else -- not on a button that
/// merely wants attention, not on a warning unrelated to either system.
/// Reaching for one because it looks good breaks an estate-wide convention; it
/// is not a local style choice.
const refSwitchyard = Color(0xFFE2623D); // coral
const refAmber = Color(0xFFD99B2B); // gold

/// Tier 1 / generated -- steel. CLAUDE.md invariant 1's read-only pane: the
/// estate's account of what exists, regenerated and never hand-edited.
const chGenerated = Color(0xFF8FA6BD);

/// A thread's resolved marker (CHRN-57). Deliberately not [chGenerated]:
/// resolving a thread is an authored, tier-2 event, not regenerated content.
const chResolved = Color(0xFF63B58C);

/// Surfaces, darkest to lightest.
const chBase = Color(0xFF08090A);
const chRaised = Color(0xFF1B1C21);
const chLine = Color(0xFF26272D);

/// Text, highest to lowest emphasis.
const chText = Color(0xFFECEBE7);
const chText2 = Color(0xFFA4A29C);
const chTextMeta = Color(0xFF6E6C67);

/// Type scale, read off board 1c by CHRN-58 and carried here unchanged. It is
/// deliberately short and MAY GROW: a screen needing a size that is not here
/// adds it rather than reaching for a bare number.
const sizeXxs = 9.5; // micro mono labels: section headers, READ ONLY stamp
const sizeXs = 10.5; // meta captions: timestamps, account email
const sizeSm = 11.5; // wordmark text, tier-1 tree items, source labels
const sizeBody = 13.0; // tree rows and list rows
const sizeMd = 14.5; // row titles: account display name, a row's primary text
const sizeBase = 15.0; // body copy baseline

/// Letter-spacing for mono micro-labels. CSS `em` is relative to font size and
/// Flutter's `letterSpacing` is absolute, so these are multiplied by the size
/// they are used at -- see [trackWide] usage in `theme.dart`.
const trackWideEm = 0.16; // loud mono micro-labels
const trackMidEm = 0.08; // meta mono captions

/// Spacing scale.
const space1 = 7.0; // tight gaps
const space2 = 10.0; // row-internal gaps
const space3 = 16.0; // horizontal rhythm: padding, indent
const space4 = 18.0; // section breaks

/// The minimum tap target the epic specifies: *"412 × 915, 44 px minimum tap
/// targets, per the canvas."* Material's own default is 48, so this is a floor
/// rather than a ceiling -- nothing interactive is smaller than this.
const minTapTarget = 44.0;

/// Type families. **A NAMED DEFERRAL, in CHRN-54's own style.**
///
/// The canvas's faces are Hanken Grotesk (interface), JetBrains Mono (labels,
/// IDs, metrics -- anywhere a value must read as exact) and Newsreader (serif
/// display, documents and print, never chrome). The web client gets them from
/// `@fontsource`, which ships **woff2 only**, and Flutter needs ttf/otf -- so
/// bundling them is a real asset-vendoring job rather than a copy, and it buys
/// nothing on the one screen CHRN-59 ships.
///
/// So this skeleton uses the platform faces, and the mono role is kept as a
/// role: code asks for [fontMono] rather than hard-coding `'monospace'`, so
/// bundling the real face later is one edit in this file. The first E9 screen
/// with a board of its own (CHRN-60's capture, CHRN-62's confirm, CHRN-63's
/// triage) is where that lands, exactly as CHRN-54 left the type scale to
/// "whichever ticket builds the first real screen and can see what sizes it
/// actually needs."
const String? fontSans = null; // platform sans
const String fontMono = 'monospace';
