/// The server address, and the one rule about it: **there is exactly one.**
///
/// Chronicle serves two hostnames off one backend and they are not
/// interchangeable (`deploy/README.md`):
///
///   * `chronicle.…` is tunneled and **Cloudflare Access-gated** — a browser
///     host. A bearer token cannot open that gate.
///   * `chronicle-direct.…` is a DNS-only A record to the WAN address with **no
///     tunnel ingress and no Access application** — the app's host, where an
///     invite is redeemed at `POST /auth/session`.
///
/// Which of its origins a phone can reach is a fact only the server knows, so
/// the server is what tells us: `CHRONICLE_MOBILE_BASE_URL` is, in its own
/// config comment, *"the only origin a phone is told about"*, and
/// `internal/invite/url.go` bakes it into the invite's `sign_in_url` precisely
/// so that no client builds the address itself. That package's comment records
/// what happens otherwise: *"which is how Lyceum's web app came to encode its
/// own Cloudflare-gated origin into a QR meant for a phone that cannot pass
/// that gate."*
///
/// So this app holds one base URL, learned by scanning, and never guesses a
/// second. "Reaches the API from both networks" is then a property of that one
/// address — Tailscale at home, the WAN record outside — and not of failover
/// logic here. See `lib/api/reachability.dart` for what the app does when the
/// address it holds cannot be reached.
library;

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'session.dart';

/// Trimmed, no trailing slash. Deliberately the same normalisation as the Go
/// `invite.NormalizeBase` and the web client's own, so a URL that round-trips
/// through the QR compares equal to one typed by hand.
String normalizeServerUrl(String url) =>
    url.trim().replaceAll(RegExp(r'/+$'), '');

const _serverKey = 'chronicle.server_url';

/// Optional compile-time default for development:
///
/// ```
/// flutter run --dart-define=CHRONICLE_BASE_URL=http://10.0.0.20:4009
/// ```
///
/// **Never in a build that leaves this machine.** An installed app must start
/// with no address, because the invite QR is what supplies one — baking a
/// default in points every install at whoever built the APK and hides the
/// connect prompt that scanning starts from. Lyceum enforces that with
/// `tool/check_store_build.sh` failing on any `--dart-define` in its release
/// workflow; Chronicle has no APK release track yet, and the ticket that adds
/// one owns that guard.
const _compileDefault = String.fromEnvironment('CHRONICLE_BASE_URL');

/// The configured base URL, normalised. Empty means "not configured yet", and
/// the router sends an empty value to the sign-in screen.
class ServerUrlController extends Notifier<String> {
  @override
  String build() {
    final saved = ref.watch(prefsProvider).getString(_serverKey);
    if (saved != null && saved.isNotEmpty) return saved;
    return normalizeServerUrl(_compileDefault);
  }

  /// Point the app at a Chronicle.
  ///
  /// **The session token is dropped when the address changes.** A session
  /// belongs to the server that issued it: it is meaningless to a different one,
  /// and keeping it is actively worse than dropping it, because everything
  /// downstream reads "we hold a token" as "we are signed in" — so a leftover
  /// credential would make a server we have never authenticated against look
  /// signed-in, and the first real request would fail as a 401 that reads like
  /// an expired session rather than like the wrong address.
  Future<void> set(String url) async {
    final normalized = normalizeServerUrl(url);
    if (normalized == state) return; // re-saving the same address is a no-op

    final prefs = ref.read(prefsProvider);
    if (normalized.isEmpty) {
      await prefs.remove(_serverKey);
    } else {
      await prefs.setString(_serverKey, normalized);
    }
    await ref.read(sessionTokenProvider.notifier).clear();
    state = normalized;
  }

  Future<void> clear() => set('');
}

final serverUrlProvider = NotifierProvider<ServerUrlController, String>(
  ServerUrlController.new,
);

/// True once an address is configured.
final hasServerProvider =
    Provider<bool>((ref) => ref.watch(serverUrlProvider).isNotEmpty);

/// Set in `main.dart` before the app builds, so the address is known on the
/// first frame and the app does not flash the sign-in screen at a signed-in
/// device.
final prefsProvider = Provider<SharedPreferences>(
  (ref) => throw StateError('prefsProvider must be overridden in main()'),
);
