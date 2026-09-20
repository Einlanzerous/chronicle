/// The session token: what it is, and where it is kept.
///
/// `POST /auth/session` returns it **once and never again** — the spec says so
/// outright, because the server stores only a hash. Losing it locally therefore
/// means redeeming a fresh invite, not re-reading it from anywhere.
///
/// It is kept in the platform keystore (`flutter_secure_storage`) and not
/// alongside the server address in `SharedPreferences`, which is a deliberate
/// split rather than tidiness: the address is a preference, and the token is a
/// **durable per-device credential** that authenticates every request the app
/// makes. `SharedPreferences` is a world-readable-by-root XML file; the keystore
/// is hardware-backed where the device has one.
library;

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

const _tokenKey = 'chronicle.session_token';

/// Injectable so tests drive the controller without a platform channel.
abstract class TokenStorage {
  Future<String?> read();
  Future<void> write(String token);
  Future<void> delete();
}

class SecureTokenStorage implements TokenStorage {
  const SecureTokenStorage(this._storage);

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read() => _storage.read(key: _tokenKey);

  @override
  Future<void> write(String token) =>
      _storage.write(key: _tokenKey, value: token);

  @override
  Future<void> delete() => _storage.delete(key: _tokenKey);
}

/// In-memory token storage, for tests.
class MemoryTokenStorage implements TokenStorage {
  String? _token;

  @override
  Future<String?> read() async => _token;

  @override
  Future<void> write(String token) async => _token = token;

  @override
  Future<void> delete() async => _token = null;
}

final tokenStorageProvider = Provider<TokenStorage>(
  (ref) => const SecureTokenStorage(FlutterSecureStorage()),
);

/// The held session token, or null when this device is not signed in.
///
/// The initial read is asynchronous, so the value starts as null and becomes the
/// stored token once the keystore answers. `main.dart` awaits [load] before the
/// first frame for exactly that reason — otherwise a signed-in device shows the
/// sign-in screen for one frame and the router bounces it back.
class SessionTokenController extends Notifier<String?> {
  @override
  String? build() => null;

  Future<void> load() async {
    final held = await ref.read(tokenStorageProvider).read();
    if (held != null && held.isNotEmpty) state = held;
  }

  Future<void> set(String token) async {
    await ref.read(tokenStorageProvider).write(token);
    state = token;
  }

  Future<void> clear() async {
    await ref.read(tokenStorageProvider).delete();
    state = null;
  }
}

final sessionTokenProvider =
    NotifierProvider<SessionTokenController, String?>(SessionTokenController.new);

/// True when a token is held. Not the same as "the token is still good" — only
/// a request can answer that, and a 401 is how the app finds out (see
/// `transport.dart`).
final isSignedInProvider = Provider<bool>(
  (ref) => (ref.watch(sessionTokenProvider) ?? '').isNotEmpty,
);
