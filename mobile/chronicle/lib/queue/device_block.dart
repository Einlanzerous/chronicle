/// The device-level send block: `signedOut` or `wrongHost`. Held in memory
/// by the engine only -- never written to any capture's `upload.json`.
///
/// These are facts about the device's session and configured host, not
/// about any one capture. Persisting them would mean a rewrite per capture
/// on every sign-in and a sweep that must find every one -- miss one and
/// the screen says `SIGN IN TO SEND` on a signed-in phone, the exact class
/// of lie CHRN-60 already ruled out for `state: recording` on disk.
library;

enum DeviceBlockReason { signedOut, wrongHost }

/// A block, valid only while the inputs it was raised against still hold.
///
/// There is deliberately no `clear()`. A block is not cleared; it is
/// SUPERSEDED by a fresh read of the two inputs it depends on. That is what
/// makes it self-correcting: hold a stale [DeviceBlock] as long as you
/// like, [stillApplies] answers honestly either way.
class DeviceBlock {
  const DeviceBlock({
    required this.reason,
    required this.serverUrl,
    required this.tokenDigest,
  });

  final DeviceBlockReason reason;

  /// The `server_url` preference at the moment this block was raised.
  final String serverUrl;

  /// A digest of the held token, or the empty string for "no token held" --
  /// never the token itself. Comparing digests rather than values keeps a
  /// session token out of a value type that might end up in a log line or a
  /// test failure message.
  final String tokenDigest;

  /// True only while both inputs are exactly what they were when this
  /// block was raised. Either changing -- a fresh sign-in, a token revoked
  /// and re-granted, a re-scan pointing at a different host -- means the
  /// fact this block was about no longer holds, whatever [reason] says.
  bool stillApplies({
    required String serverUrl,
    required String tokenDigest,
  }) =>
      this.serverUrl == serverUrl && this.tokenDigest == tokenDigest;
}
