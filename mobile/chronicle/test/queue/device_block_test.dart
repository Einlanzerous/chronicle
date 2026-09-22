import 'package:chronicle/queue/device_block.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('DeviceBlock.stillApplies', () {
    const block = DeviceBlock(
      reason: DeviceBlockReason.signedOut,
      serverUrl: 'https://chronicle-direct.example.com',
      tokenDigest: '',
    );

    test('applies while both inputs match exactly', () {
      expect(
        block.stillApplies(
          serverUrl: 'https://chronicle-direct.example.com',
          tokenDigest: '',
        ),
        isTrue,
      );
    });

    test('a token appearing (digest changes) lifts the block', () {
      expect(
        block.stillApplies(
          serverUrl: 'https://chronicle-direct.example.com',
          tokenDigest: 'sha256-of-a-real-token',
        ),
        isFalse,
      );
    });

    test('a changed server URL lifts a wrongHost block', () {
      const wrongHost = DeviceBlock(
        reason: DeviceBlockReason.wrongHost,
        serverUrl: 'https://chronicle.example.com',
        tokenDigest: 'sha256-token',
      );
      expect(
        wrongHost.stillApplies(
          serverUrl: 'https://chronicle-direct.example.com',
          tokenDigest: 'sha256-token',
        ),
        isFalse,
      );
    });

    test('there is no clear() -- a block is only ever superseded', () {
      // Documented by absence: this test exists so a future edit that adds
      // a mutating clear() has something to delete alongside it.
      expect(block.reason, DeviceBlockReason.signedOut);
    });
  });
}
