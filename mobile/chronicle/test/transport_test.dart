import 'dart:async';

import 'package:chronicle/api/transport.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  group('ChronicleClient attaches the credential', () {
    test('as a bearer header when a token is held', () async {
      String? seen;
      final client = ChronicleClient(
        token: 'tok-123',
        inner: MockClient((req) async {
          seen = req.headers['Authorization'];
          return http.Response('{}', 200);
        }),
      );
      await client.get(Uri.parse('https://host/auth/me'));
      expect(seen, 'Bearer tok-123');
    });

    test('and sends no Authorization header when signed out', () async {
      var hadHeader = true;
      final client = ChronicleClient(
        inner: MockClient((req) async {
          hadHeader = req.headers.containsKey('Authorization');
          return http.Response('{}', 200);
        }),
      );
      await client.get(Uri.parse('https://host/healthz'));
      // /healthz takes no credential, and onboarding probes it before one
      // exists. Sending an empty bearer would be a malformed request.
      expect(hadHeader, isFalse);
    });

    test('and never sends an empty bearer for an empty token', () async {
      var hadHeader = true;
      final client = ChronicleClient(
        token: '',
        inner: MockClient((req) async {
          hadHeader = req.headers.containsKey('Authorization');
          return http.Response('{}', 200);
        }),
      );
      await client.get(Uri.parse('https://host/healthz'));
      expect(hadHeader, isFalse);
    });
  });

  group('ChronicleClient refuses redirects', () {
    test('turns redirect-following off, so the redirect reaches us', () async {
      bool? followed;
      final client = ChronicleClient(
        inner: MockClient((req) async {
          followed = req.followRedirects;
          return http.Response('{}', 200);
        }),
      );
      await client.get(Uri.parse('https://host/healthz'));
      // Left on, `http` would follow Access's 302 and hand back a 200 whose
      // body is a login page -- which the generated client then reports as a
      // parse error, blaming the server for the wrong host.
      expect(followed, isFalse);
    });

    test('diagnoses a Cloudflare Access login as the gated host', () async {
      final client = ChronicleClient(
        inner: MockClient((req) async => http.Response('', 302, headers: {
              'location':
                  'https://zerogravity.cloudflareaccess.com/cdn-cgi/access/login/chronicle.zerogravity.industries',
            })),
      );

      await expectLater(
        client.get(Uri.parse('https://chronicle.zerogravity.industries/auth/me')),
        throwsA(isA<NotChronicleException>()
            .having((e) => e.isAccessGated, 'isAccessGated', isTrue)
            .having((e) => e.statusCode, 'statusCode', 302)),
      );
    });

    test('recognises the Access path even without the team domain', () async {
      final client = ChronicleClient(
        inner: MockClient((req) async => http.Response('', 302,
            headers: {'location': '/cdn-cgi/access/login'})),
      );
      await expectLater(
        client.get(Uri.parse('https://host/auth/me')),
        throwsA(isA<NotChronicleException>()
            .having((e) => e.isAccessGated, 'isAccessGated', isTrue)),
      );
    });

    test('refuses an unattributable redirect without calling it Access',
        () async {
      final client = ChronicleClient(
        inner: MockClient((req) async => http.Response('', 301,
            headers: {'location': 'https://elsewhere.example/'})),
      );
      // Still refused -- openapi.yaml declares no 3xx on any operation -- but
      // NOT diagnosed as Access, because we do not know that.
      await expectLater(
        client.get(Uri.parse('https://host/auth/me')),
        throwsA(isA<NotChronicleException>()
            .having((e) => e.isAccessGated, 'isAccessGated', isFalse)),
      );
    });

    test('lets a 304 through, because it is a cache validator', () async {
      // The only 3xx the contract does declare. Refusing it would break the
      // conditional GETs on /audio and the note routes.
      final client = ChronicleClient(
        inner: MockClient((req) async => http.Response('', 304)),
      );
      final res = await client.get(Uri.parse('https://host/audio/m1'));
      expect(res.statusCode, 304);
    });
  });

  group('ChronicleClient bounds the wait', () {
    test('a server that never answers surfaces a TimeoutException', () async {
      final client = ChronicleClient(
        timeout: const Duration(milliseconds: 50),
        inner: MockClient((req) async {
          await Future<void>.delayed(const Duration(seconds: 30));
          return http.Response('{}', 200);
        }),
      );
      // Dart's http.Client never times out on its own: without the deadline this
      // would hang for 30s and the UI would sit on a spinner instead of saying
      // it cannot reach the server.
      await expectLater(
        client.get(Uri.parse('https://host/healthz')),
        throwsA(isA<TimeoutException>()),
      );
    });
  });
}
