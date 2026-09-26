/// Wire-format tests for [MemosApiAudioGateTransport]: what actually goes out
/// over HTTP and what comes back, intercepted with `MockClient` -- the same
/// approach as `uploads_api_transport_test.dart`.
///
/// The property that matters is the one the generated `getMemoAudio` would have
/// broken: the status IS the answer here (the delete signal is a `410`), so a
/// non-2xx must come back as a value, not be thrown away.
library;

import 'dart:io';

import 'package:chronicle/queue/audio_gate_transport.dart';
import 'package:chronicle_api/api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  MemosApiAudioGateTransport over(http.Client client) => MemosApiAudioGateTransport(
      MemosApi(ApiClient(basePath: 'https://chronicle-direct.example.com')..client = client));

  test('asks for exactly one byte of GET /audio/{memo_id}', () async {
    http.BaseRequest? captured;
    final transport = over(MockClient((request) async {
      captured = request;
      return http.Response('x', 206, headers: {'content-range': 'bytes 0-0/1234'});
    }));

    final response = await transport.probe('0198a7c2-aaaa-bbbb-cccc-000000000001');

    expect(captured!.method, 'GET');
    expect(captured!.url.path, '/audio/0198a7c2-aaaa-bbbb-cccc-000000000001');
    expect(captured!.headers['Range'], 'bytes=0-0');
    expect(response.statusCode, 206);
    expect(response.body, isNull, reason: 'a success carries audio, which is never read');
  });

  test('a 410 comes back as a value with its body, not as an exception', () async {
    final transport = over(MockClient((_) async => http.Response(
          '{"code":"audio_pruned","message":"this recording was pruned"}',
          410,
          headers: {'content-type': 'application/json'},
        )));

    final response = await transport.probe('m');

    expect(response.statusCode, 410);
    expect(response.body, contains('audio_pruned'));
  });

  test('every error status is returned rather than thrown', () async {
    for (final status in [400, 401, 404, 429, 500, 503]) {
      final transport = over(MockClient((_) async => http.Response('{"code":"c"}', status)));
      final response = await transport.probe('m');
      expect(response.statusCode, status);
      expect(response.body, '{"code":"c"}');
    }
  });

  test('a bare 200 (Range not honoured) is returned as such, without reading the '
      'body into the answer', () async {
    final transport = over(MockClient((_) async => http.Response('AUDIO' * 100, 200)));

    final response = await transport.probe('m');

    expect(response.statusCode, 200);
    expect(response.body, isNull);
  });

  test('a failure to get any answer at all is thrown, for classifyProbeFailure',
      () async {
    final transport = over(MockClient((_) async => throw const SocketException('down')));

    expect(() => transport.probe('m'), throwsA(anything));
  });
}
