import 'package:chronicle/router/router.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('sign-in gates sending, never capturing or seeing what was captured', () {
    test('a device with no credential may still reach capture', () {
      // The clause CHRN-60 exists to protect: the epic's invariant is that
      // capture never depends on the network, and a session token is a network
      // fact. A revoked device must still be able to record.
      expect(redirectFor(location: captureRoute, ready: false), isNull);
    });

    test('a device with no credential may still reach the queue', () {
      // CHRN-61: a device that recorded offline before ever signing in still
      // needs to see what it is holding and why it says SIGN IN TO SEND.
      expect(redirectFor(location: queueRoute, ready: false), isNull);
    });

    test('capture and the queue are left alone however credentials change', () {
      // Nothing clears the token from a background retry -- see
      // router.dart's own library doc. This is simply the same escape,
      // exercised at both states for the record.
      expect(redirectFor(location: captureRoute, ready: true), isNull);
      expect(redirectFor(location: captureRoute, ready: false), isNull);
      expect(redirectFor(location: queueRoute, ready: true), isNull);
      expect(redirectFor(location: queueRoute, ready: false), isNull);
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
