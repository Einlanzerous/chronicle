import 'dart:async';

import 'package:chronicle/api/reachability.dart';
import 'package:chronicle/api/server_url.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// CHRN-59's third clause: *"recovers cleanly when the tunnel is down."*
///
/// Two things are being proved. First that the app can tell the failures apart,
/// because "something went wrong" for a wrong host spends the operator's trust
/// as fast as a hang does. Second, and the actual word in the `Done when`, that
/// **nothing latches**: a failure is a value, not a mode, so a later probe that
/// succeeds returns the app to connected with no restart.
void main() {
  Future<ProviderContainer> containerWith(http.Client client) async {
    SharedPreferences.setMockInitialValues(
      {'chronicle.server_url': 'https://chronicle-direct.example'},
    );
    final prefs = await SharedPreferences.getInstance();
    return ProviderContainer(overrides: [
      prefsProvider.overrideWithValue(prefs),
      probeClientProvider.overrideWithValue(client),
    ]);
  }

  group('probeServer classifies', () {
    test('a 200 from /healthz as ok, and probes the right path', () async {
      Uri? asked;
      final status = await probeServer(
        'https://host',
        client: MockClient((req) async {
          asked = req.url;
          return http.Response('{"status":"ok"}', 200);
        }),
      );
      expect(status.reach, Reach.ok);
      expect(status.isOk, isTrue);
      expect(asked.toString(), 'https://host/healthz');
      // The age is always carried: a status with no timestamp is not a claim
      // about now.
      expect(status.checkedAt, isNotNull);
    });

    test('a trailing slash on the stored address, without doubling it', () async {
      Uri? asked;
      await probeServer(
        'https://host/',
        client: MockClient((req) async {
          asked = req.url;
          return http.Response('{}', 200);
        }),
      );
      expect(asked.toString(), 'https://host/healthz');
    });

    test('a dead address as unreachable', () async {
      final status = await probeServer(
        'https://host',
        client: MockClient((_) async => throw http.ClientException('no route')),
      );
      expect(status.reach, Reach.unreachable);
      expect(status.isTransient, isTrue);
    });

    test('a silent address as timedOut, not unreachable', () async {
      final status = await probeServer(
        'https://host',
        timeout: const Duration(milliseconds: 50),
        client: MockClient((_) async {
          await Future<void>.delayed(const Duration(seconds: 30));
          return http.Response('{}', 200);
        }),
      );
      // Told apart because the advice differs: a timeout is worth retrying on
      // the spot, a refused connection usually is not.
      expect(status.reach, Reach.timedOut);
      expect(status.isTransient, isTrue);
    });

    test('the Access-gated host as a configuration problem, not a network one',
        () async {
      final status = await probeServer(
        'https://chronicle.zerogravity.industries',
        client: MockClient((_) async => http.Response('', 302, headers: {
              'location':
                  'https://zerogravity.cloudflareaccess.com/cdn-cgi/access/login/x',
            })),
      );
      expect(status.reach, Reach.accessGated);
      // Retrying cannot fix it, so the UI must not offer "try again" -- it
      // would send the operator in a circle.
      expect(status.isTransient, isFalse);
      expect(status.message, contains('direct host'));
    });

    test('an unattributable redirect without blaming Access', () async {
      final status = await probeServer(
        'https://host',
        client: MockClient((_) async => http.Response('', 307,
            headers: {'location': 'https://elsewhere.example/'})),
      );
      expect(status.reach, Reach.unexpectedRedirect);
    });

    test('a 5xx as the server\'s problem', () async {
      final status = await probeServer(
        'https://host',
        client: MockClient((_) async => http.Response('boom', 503)),
      );
      expect(status.reach, Reach.serverError);
      expect(status.statusCode, 503);
      expect(status.isTransient, isTrue);
    });

    test('a 404 as "not a Chronicle", not as unreachable', () async {
      final status = await probeServer(
        'https://host',
        client: MockClient((_) async => http.Response('nope', 404)),
      );
      // Something answered. Calling this "cannot reach the server" would send
      // the operator to look at their signal when the address is wrong.
      expect(status.reach, Reach.notChronicle);
      expect(status.isTransient, isFalse);
    });

    test('an address that is not a URL', () async {
      final status = await probeServer(
        'not a url',
        client: MockClient((_) async => http.Response('{}', 200)),
      );
      expect(status.reach, Reach.malformedAddress);
      expect(status.isTransient, isFalse);
    });
  });

  group('the controller', () {
    test('starts unknown rather than claiming a failure it has not checked',
        () async {
      final container = await containerWith(
        MockClient((_) async => http.Response('{}', 200)),
      );
      addTearDown(container.dispose);
      expect(container.read(reachabilityProvider).reach, Reach.unknown);
      expect(container.read(reachabilityProvider).checkedAt, isNull);
    });

    test('RECOVERS: down, then up, with no restart and no latched error',
        () async {
      // One client whose behaviour changes under the app, which is what a tunnel
      // going down and coming back actually looks like from the device.
      var up = false;
      final container = await containerWith(MockClient((_) async {
        if (!up) throw http.ClientException('no route to host');
        return http.Response('{"status":"ok"}', 200);
      }));
      addTearDown(container.dispose);

      final down = await container.read(reachabilityProvider.notifier).check();
      expect(down.reach, Reach.unreachable);
      expect(container.read(reachabilityProvider).reach, Reach.unreachable);

      up = true;

      final back = await container.read(reachabilityProvider.notifier).check();
      expect(back.reach, Reach.ok, reason: 'a failure must not latch');
      expect(container.read(reachabilityProvider).isOk, isTrue);
      expect(
        container.read(reachabilityProvider).checkedAt,
        isNotNull,
        reason: 'the recovered status carries its own, newer timestamp',
      );
    });

    test('and back down again, so recovery is not one-way either', () async {
      var up = true;
      final container = await containerWith(MockClient((_) async {
        if (!up) throw http.ClientException('gone');
        return http.Response('{}', 200);
      }));
      addTearDown(container.dispose);

      expect((await container.read(reachabilityProvider.notifier).check()).isOk,
          isTrue);
      up = false;
      expect((await container.read(reachabilityProvider.notifier).check()).reach,
          Reach.unreachable);
    });

    test('says so rather than probing when no address is configured', () async {
      SharedPreferences.setMockInitialValues({});
      final prefs = await SharedPreferences.getInstance();
      var probed = false;
      final container = ProviderContainer(overrides: [
        prefsProvider.overrideWithValue(prefs),
        probeClientProvider.overrideWithValue(MockClient((_) async {
          probed = true;
          return http.Response('{}', 200);
        })),
      ]);
      addTearDown(container.dispose);

      final status = await container.read(reachabilityProvider.notifier).check();
      expect(status.reach, Reach.malformedAddress);
      expect(probed, isFalse, reason: 'nothing to probe without an address');
    });
  });
}
