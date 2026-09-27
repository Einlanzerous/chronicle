/// The three answers CHRN-62's confirm offers, and the wire value each one
/// declares.
///
/// `meta.json` stores the wire string (`CaptureRecord.retention`), because
/// that is what `uploads_api_transport.dart` already maps to the generated
/// enum and what every record written before this file existed would carry.
/// This enum is the screen's vocabulary for the same three strings.
library;

enum RetentionChoice {
  /// Pruned at the server's next sweep once a durable transcript exists --
  /// never before, because pruning audio whose transcription never succeeded
  /// is the one loss this system cannot recover from.
  discardNow('discard_now', 'DISCARD NOW', 'Discard once transcribed',
      'transcript is permanent · audio goes at the next sweep'),

  /// The deployment default, and the confirm's fast path.
  days30('days_30', '30 DAYS', 'Keep for 30 days',
      'transcript is permanent · audio prunes at 30 days'),

  /// Pinned: never pruned.
  forever('forever', 'FOREVER', 'Keep forever',
      'transcript is permanent · audio is pinned');

  const RetentionChoice(this.wire, this.chip, this.title, this.detail);

  /// The value `meta.json` and `POST /memos/uploads` carry.
  final String wire;

  /// The chip's words, exactly as board 1a draws them.
  final String chip;

  /// The line above the chips, for the choice currently selected.
  final String title;
  final String detail;

  /// Null for anything that is not one of the three -- including null, which
  /// is "no opinion", never `days30`.
  static RetentionChoice? fromWire(String? wire) {
    for (final c in values) {
      if (c.wire == wire) return c;
    }
    return null;
  }
}
