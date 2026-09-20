import 'package:chronicle/router/router.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('sign-in gates sending, never capturing', () {
    test('a device with no credential may still reach capture', () {
      // The clause CHRN-60 exists to protect: the epic's invariant is that
      // capture never depends on the network, and a session token is a network
      // fact. A revoked device must still be able to record.
      expect(redirectFor(location: captureRoute, ready: false), isNull);
    });

    test('a device that loses its credential is not bounced off capture', () {
      // A 401 on a background upload clears the token (see transport.dart), and
      // the router rebuilds on that. Without the capture clause this would
      // throw somebody off the RECORDING screen mid-memo.
      expect(redirectFor(location: captureRoute, ready: true), isNull);
      expect(redirectFor(location: captureRoute, ready: false), isNull);
    });

    test('every other route without a credential goes to the front door', () {
      expect(redirectFor(location: '/', ready: false), '/sign-in');
    });

    test('the front door itself is not a redirect loop', () {
      expect(redirectFor(location: '/sign-in', ready: false), isNull);
    });

    test('a signed-in device is moved off the front door', () {
      expect(redirectFor(location: '/sign-in', ready: true), '/');
    });

    test('a signed-in device is left alone everywhere else', () {
      expect(redirectFor(location: '/', ready: true), isNull);
    });
  });
}
