/// Riverpod wiring: one generated client, rebuilt when the address or the
/// credential changes.
library;

import 'package:chronicle_api/api.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'server_url.dart';
import 'session.dart';
import 'transport.dart';

/// The generated [ApiClient], pointed at the configured server and carrying the
/// held token.
///
/// It **watches** both, so changing either rebuilds the client rather than
/// leaving a stale one holding a revoked token or the previous address. The
/// generated client's default `basePath` is the in-cluster address from
/// `openapi.yaml`'s `servers` block, which no phone can resolve — it is
/// overridden here on every construction, never defaulted to.
final apiClientProvider = Provider<ApiClient>((ref) {
  final baseUrl = ref.watch(serverUrlProvider);
  final token = ref.watch(sessionTokenProvider);

  final transport = ChronicleClient(token: token);
  ref.onDispose(transport.close);

  return ApiClient(basePath: baseUrl)..client = transport;
});

final authApiProvider = Provider<AuthApi>(
  (ref) => AuthApi(ref.watch(apiClientProvider)),
);

final metaApiProvider = Provider<MetaApi>(
  (ref) => MetaApi(ref.watch(apiClientProvider)),
);

/// CHRN-61's upload queue. Watches [apiClientProvider], so a sign-in, a
/// sign-out or a re-scanned server address rebuilds it exactly like every
/// other API surface here -- the queue never holds a stale credential.
final uploadsApiProvider = Provider<UploadsApi>(
  (ref) => UploadsApi(ref.watch(apiClientProvider)),
);

/// CHRN-120's prune pass asks the server one question about each acknowledged
/// memo (`GET /audio/{id}`), through this. Watches [apiClientProvider] for the
/// same reason [uploadsApiProvider] does.
final memosApiProvider = Provider<MemosApi>(
  (ref) => MemosApi(ref.watch(apiClientProvider)),
);
