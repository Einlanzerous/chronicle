import 'package:chronicle/auth/sign_in_link.dart';
import 'package:flutter_test/flutter_test.dart';

/// The QR is how a device learns BOTH facts it needs, so the parser is the one
/// place a typo becomes "cannot reach the server" about an address that was
/// never a server. These cases are the ones a scanner actually hands us.
void main() {
  group('parseSignInLink accepts', () {
    test('the link the server mints', () {
      final link = parseSignInLink(
        'https://chronicle-direct.zerogravity.industries/sign-in?token=abc123',
      );
      expect(link, isNotNull);
      expect(link!.baseUrl,
          'https://chronicle-direct.zerogravity.industries');
      expect(link.token, 'abc123');
    });

    test('a percent-encoded token, decoded once', () {
      // invite.SignInURL runs the token through url.QueryEscape, so anything
      // needing encoding arrives encoded and must be redeemed decoded.
      final link = parseSignInLink('https://host/sign-in?token=a%2Bb%3Dc');
      expect(link!.token, 'a+b=c');
    });

    test('an explicit port', () {
      final link = parseSignInLink('http://10.0.0.20:4009/sign-in?token=t');
      expect(link!.baseUrl, 'http://10.0.0.20:4009');
      expect(link.token, 't');
    });

    test('a deployment served under a subpath', () {
      final link =
          parseSignInLink('https://host/chronicle/sign-in?token=t');
      expect(link!.baseUrl, 'https://host/chronicle');
    });

    test('extra query parameters alongside the token', () {
      final link = parseSignInLink('https://host/sign-in?utm=qr&token=t');
      expect(link!.token, 't');
    });

    test('surrounding whitespace, which chat apps and readers add', () {
      final link = parseSignInLink('  https://host/sign-in?token=t\n');
      expect(link!.baseUrl, 'https://host');
      expect(link.token, 't');
    });
  });

  group('parseSignInLink rejects', () {
    test('an unrelated QR code', () {
      // The failure this guards: storing a base URL read off a Wi-Fi credential
      // or a parcel tracking link, then reporting a connection problem about it.
      expect(parseSignInLink('WIFI:S:home;T:WPA;P:hunter2;;'), isNull);
      expect(parseSignInLink('https://example.com/track/9f3a'), isNull);
    });

    test('a sign-in path with no token', () {
      expect(parseSignInLink('https://host/sign-in'), isNull);
      expect(parseSignInLink('https://host/sign-in?token='), isNull);
      expect(parseSignInLink('https://host/sign-in?token=%20'), isNull);
    });

    test('a non-http scheme', () {
      expect(parseSignInLink('chronicle://host/sign-in?token=t'), isNull);
      expect(parseSignInLink('file:///sign-in?token=t'), isNull);
    });

    test('a token on the wrong path', () {
      expect(parseSignInLink('https://host/login?token=t'), isNull);
      // /sign-in must be the LAST segment; a deeper path is not the mint route.
      expect(parseSignInLink('https://host/sign-in/extra?token=t'), isNull);
    });

    test('empty input', () {
      expect(parseSignInLink(''), isNull);
      expect(parseSignInLink('   '), isNull);
    });
  });
}
