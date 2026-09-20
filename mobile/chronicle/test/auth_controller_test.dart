import 'dart:convert';

import 'package:chronicle/api/providers.dart';
import 'package:chronicle/api/reachability.dart';
import 'package:chronicle/api/server_url.dart';
import 'package:chronicle/api/session.dart';
import 'package:chronicle/auth/auth_controller.dart';
import 'package:chronicle_api/api.dart' as gen;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// CHRN-59's first clause: *"the app authenticates."*
void main() {
  const sessionBody = {
    'user': {
      'id': '11111111-1111-1111-1111-111111111111',
      'email': 'magos@example.test',
      'display_name': 'magos',
      'kind': 'person',
      'is_owner': true,
    },
    'session_token': 'sess-abc',
  };

  /// A container whose HTTP is entirely stubbed: [probe] answers `/healthz`
  /// (the reachability pre-flight) and [api] answers everything the generated
  /// client sends.
  Future<ProviderContainer> harness({
    required http.Client probe,
    required http.Client api,
    Map<String, Object> prefs = const {},
  }) async {
    SharedPreferences.setMockInitialValues(Map.of(prefs));
    final loaded = await SharedPreferences.getInstance();
    return ProviderContainer(overrides: [
      prefsProvider.overrideWithValue(loaded),
      probeClientProvider.overrideWithValue(probe),
      tokenStorageProvider.overrideWithValue(MemoryTokenStorage()),
      apiClientProvider.overrideWithValue(
        gen.ApiClient(basePath: 'https://chronicle-direct.example')..client = api,
      ),
    ]);
  }

  http.Client healthyProbe() =>
      MockClient((_) async => http.Response('{"status":"ok"}', 200));

  test('a scanned invite becomes a stored session', () async {
    final requests = <http.Request>[];
    final container = await harness(
      probe: healthyProbe(),
      api: MockClient((req) async {
        requests.add(req);
        return http.Response(jsonEncode(sessionBody), 200,
            headers: {'content-type': 'application/json'});
      }),
    );
    addTearDown(container.dispose);

    final result = await container.read(authControllerProvider.notifier).signIn(
          baseUrl: 'https://chronicle-direct.example',
          inviteToken: 'invite-1',
          label: 'Pixel 8',
        );

    expect(result.ok, isTrue);
    expect(result.user?.displayName, 'magos');

    // The address and the credential are both persisted, and the credential is
    // the one the server minted -- not the invite that bought it.
    expect(container.read(serverUrlProvider), 'https://chronicle-direct.example');
    expect(container.read(sessionTokenProvider), 'sess-abc');
    expect(container.read(isSignedInProvider), isTrue);

    expect(requests.single.url.path, '/auth/session');
    final sent = jsonDecode(requests.single.body) as Map<String, dynamic>;
    expect(sent['token'], 'invite-1');
    expect(sent['device_label'], 'Pixel 8');
  });

  test('a hard-wrapped pasted token is stripped, not merely trimmed', () async {
    final requests = <http.Request>[];
    final container = await harness(
      probe: healthyProbe(),
      api: MockClient((req) async {
        requests.add(req);
        return http.Response(jsonEncode(sessionBody), 200,
            headers: {'content-type': 'application/json'});
      }),
    );
    addTearDown(container.dispose);

    await container.read(authControllerProvider.notifier).signIn(
          baseUrl: 'https://chronicle-direct.example',
          // What a token looks like out of a chat app or a terminal log.
          inviteToken: 'inv ite\n-1 ',
          label: 'x',
        );
    final sent = jsonDecode(requests.single.body) as Map<String, dynamic>;
    expect(sent['token'], 'invite-1');
  });

  test('an Access-gated host fails WITHOUT spending the invite', () async {
    var posted = false;
    final container = await harness(
      probe: MockClient((_) async => http.Response('', 302, headers: {
            'location':
                'https://zerogravity.cloudflareaccess.com/cdn-cgi/access/login/x',
          })),
      api: MockClient((_) async {
        posted = true;
        return http.Response(jsonEncode(sessionBody), 200);
      }),
    );
    addTearDown(container.dispose);

    final result = await container.read(authControllerProvider.notifier).signIn(
          baseUrl: 'https://chronicle.zerogravity.industries',
          inviteToken: 'invite-1',
          label: 'x',
        );

    expect(result.ok, isFalse);
    expect(result.failure, SignInFailure.serverUnreachable);
    expect(result.status?.reach, Reach.accessGated);
    expect(result.message, contains('direct host'));

    // The point of probing first. An invite is single-use, so a POST into a
    // login page would burn it and the operator would need a fresh one for a
    // mistake that one unauthenticated GET rules out.
    expect(posted, isFalse);
    expect(container.read(isSignedInProvider), isFalse);
  });

  test('a spent invite is reported as spent-or-expired, never guessed at',
      () async {
    final container = await harness(
      probe: healthyProbe(),
      api: MockClient((_) async => http.Response('{"code":"unauthorized"}', 401)),
    );
    addTearDown(container.dispose);

    final result = await container.read(authControllerProvider.notifier).signIn(
          baseUrl: 'https://chronicle-direct.example',
          inviteToken: 'spent',
          label: 'x',
        );
    expect(result.failure, SignInFailure.inviteRejected);
    // The server answers spent, expired and unknown identically and
    // deliberately, so the copy must not pick one of the three.
    expect(result.message, contains('used or has expired'));
    expect(container.read(isSignedInProvider), isFalse);
  });

  test('the sign-in rate limit is its own answer', () async {
    final container = await harness(
      probe: healthyProbe(),
      api: MockClient((_) async => http.Response('{}', 429)),
    );
    addTearDown(container.dispose);

    final result = await container.read(authControllerProvider.notifier).signIn(
          baseUrl: 'https://chronicle-direct.example',
          inviteToken: 't',
          label: 'x',
        );
    expect(result.failure, SignInFailure.rateLimited);
  });

  test('a 200 carrying no token is refused rather than stored', () async {
    final container = await harness(
      probe: healthyProbe(),
      api: MockClient((_) async => http.Response(
          jsonEncode({'user': sessionBody['user'], 'session_token': ''}), 200,
          headers: {'content-type': 'application/json'})),
    );
    addTearDown(container.dispose);

    final result = await container.read(authControllerProvider.notifier).signIn(
          baseUrl: 'https://chronicle-direct.example',
          inviteToken: 't',
          label: 'x',
        );
    // Storing an empty string would make every later request present an empty
    // bearer and read as a session that had ended.
    expect(result.ok, isFalse);
    expect(container.read(isSignedInProvider), isFalse);
  });

  group('signOut', () {
    test('clears locally and reports the revocation when the server answers',
        () async {
      final container = await harness(
        probe: healthyProbe(),
        api: MockClient((req) async {
          if (req.method == 'POST') {
            return http.Response(jsonEncode(sessionBody), 200,
                headers: {'content-type': 'application/json'});
          }
          return http.Response('', 204);
        }),
      );
      addTearDown(container.dispose);

      await container.read(authControllerProvider.notifier).signIn(
          baseUrl: 'https://chronicle-direct.example',
          inviteToken: 't',
          label: 'x');
      expect(container.read(isSignedInProvider), isTrue);

      final revoked =
          await container.read(authControllerProvider.notifier).signOut();
      expect(revoked, isTrue);
      expect(container.read(isSignedInProvider), isFalse);
    });

    test('still clears locally when the server cannot be reached, and says so',
        () async {
      var signedIn = false;
      final container = await harness(
        probe: healthyProbe(),
        api: MockClient((req) async {
          if (req.method == 'POST') {
            signedIn = true;
            return http.Response(jsonEncode(sessionBody), 200,
                headers: {'content-type': 'application/json'});
          }
          throw http.ClientException('offline');
        }),
      );
      addTearDown(container.dispose);

      await container.read(authControllerProvider.notifier).signIn(
          baseUrl: 'https://chronicle-direct.example',
          inviteToken: 't',
          label: 'x');
      expect(signedIn, isTrue);

      final revoked =
          await container.read(authControllerProvider.notifier).signOut();
      // Refusing to sign out while offline would strand someone handing the
      // device over, and a session they cannot revoke is not made safer by the
      // app going on using it. The caller is told, so it can say so.
      expect(revoked, isFalse);
      expect(container.read(isSignedInProvider), isFalse);
    });
  });

  test('pointing the app at a different server drops the session', () async {
    final container = await harness(
      probe: healthyProbe(),
      api: MockClient((_) async => http.Response(jsonEncode(sessionBody), 200,
          headers: {'content-type': 'application/json'})),
    );
    addTearDown(container.dispose);

    await container.read(authControllerProvider.notifier).signIn(
        baseUrl: 'https://chronicle-direct.example',
        inviteToken: 't',
        label: 'x');
    expect(container.read(isSignedInProvider), isTrue);

    await container.read(serverUrlProvider.notifier).set('https://other.example');

    // A session belongs to the server that issued it. Keeping it would make a
    // server we have never authenticated against look signed-in, and the first
    // request would fail as a 401 that reads like an expired session rather
    // than like the wrong address.
    expect(container.read(isSignedInProvider), isFalse);
    expect(container.read(serverUrlProvider), 'https://other.example');
  });

  test('re-saving the same address is a no-op and keeps the session', () async {
    final container = await harness(
      probe: healthyProbe(),
      api: MockClient((_) async => http.Response(jsonEncode(sessionBody), 200,
          headers: {'content-type': 'application/json'})),
    );
    addTearDown(container.dispose);

    await container.read(authControllerProvider.notifier).signIn(
        baseUrl: 'https://chronicle-direct.example',
        inviteToken: 't',
        label: 'x');
    // Including a trailing slash, which normalises to the same address -- a
    // second scan of the same code must not sign the device out.
    await container
        .read(serverUrlProvider.notifier)
        .set('https://chronicle-direct.example/');
    expect(container.read(isSignedInProvider), isTrue);
  });
}
