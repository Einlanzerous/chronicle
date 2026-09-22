/// Wire-format tests for [UploadsApiTransport]: what actually goes out over
/// HTTP, intercepted with `package:http/testing.dart`'s `MockClient` rather
/// than a live server -- the plan's finding 5 (raw bytes, an exact
/// `Content-Type`, a real `Content-Length`) checked directly, closing the
/// gap between `engine_test.dart` (which only ever sees the [UploadTransport]
/// interface) and the generated client underneath it. Criterion 13's real
/// `chronicle serve` pass is a separate, manual step -- this is what proves
/// the request this client actually sends matches the contract without one.
library;

import 'dart:convert';

import 'package:chronicle/queue/uploads_api_transport.dart';
import 'package:chronicle_api/api.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

void main() {
  group('openUpload', () {
    test('POSTs the declaration as JSON to /memos/uploads', () async {
      http.Request? captured;
      final client = MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'status': 'incomplete',
            'upload_id': 'up-1',
            'byte_size': 10,
            'offset': 0,
            'duplicate': false,
          }),
          201,
          headers: {'content-type': 'application/json'},
        );
      });
      final apiClient = ApiClient(basePath: 'https://chronicle-direct.example.com')
        ..client = client;
      final transport = UploadsApiTransport(UploadsApi(apiClient));

      final state = await transport.openUpload(
        idempotencyKey: 'chr-cap-a',
        contentHash: 'deadbeef',
        byteSize: 10,
        retention: 'days_30',
      );

      expect(state.status, UploadStateStatusEnum.incomplete);
      expect(state.uploadId, 'up-1');
      expect(captured, isNotNull);
      expect(captured!.method, 'POST');
      expect(captured!.url.path, '/memos/uploads');
      expect(captured!.headers['Content-Type'], contains('application/json'));

      final body = jsonDecode(captured!.body) as Map<String, Object?>;
      expect(body['idempotency_key'], 'chr-cap-a');
      expect(body['content_hash'], 'deadbeef');
      expect(body['byte_size'], 10);
      expect(body['retention'], 'days_30');
    });

    test('no retention opinion is OMITTED, never sent as a default', () async {
      http.Request? captured;
      final client = MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'status': 'incomplete',
            'upload_id': 'up-1',
            'byte_size': 10,
            'offset': 0,
            'duplicate': false,
          }),
          201,
        );
      });
      final apiClient = ApiClient(basePath: 'https://chronicle-direct.example.com')
        ..client = client;
      final transport = UploadsApiTransport(UploadsApi(apiClient));

      await transport.openUpload(idempotencyKey: 'chr-cap-a', contentHash: 'deadbeef', byteSize: 10);

      final body = jsonDecode(captured!.body) as Map<String, Object?>;
      expect(body.containsKey('retention'), isTrue);
      expect(body['retention'], isNull);
    });

    test('a real refusal (409 idempotency_key_reused) surfaces as ApiException, untouched',
        () async {
      final client = MockClient((request) async => http.Response(
            jsonEncode({'code': 'idempotency_key_reused', 'message': 'nope'}),
            409,
          ));
      final apiClient = ApiClient(basePath: 'https://chronicle-direct.example.com')
        ..client = client;
      final transport = UploadsApiTransport(UploadsApi(apiClient));

      await expectLater(
        transport.openUpload(idempotencyKey: 'a', contentHash: 'x', byteSize: 1),
        throwsA(isA<ApiException>()
            .having((e) => e.code, 'code', 409)
            .having((e) => e.message, 'message', contains('idempotency_key_reused'))),
      );
    });
  });

  group('appendChunk', () {
    test('PATCHes the raw bytes with an exact Content-Type and the Upload-Offset header',
        () async {
      final chunk = List.generate(37, (i) => i % 256);
      http.Request? captured;
      final client = MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'status': 'incomplete',
            'upload_id': 'up-1',
            'byte_size': 100,
            'offset': 37,
            'duplicate': false,
          }),
          200,
        );
      });
      final apiClient = ApiClient(basePath: 'https://chronicle-direct.example.com')
        ..client = client;
      final transport = UploadsApiTransport(UploadsApi(apiClient));

      final state = await transport.appendChunk(uploadId: 'up-1', offset: 0, bytes: chunk);

      expect(state.offset, 37);
      expect(captured, isNotNull);
      expect(captured!.method, 'PATCH');
      expect(captured!.url.path, '/memos/uploads/up-1');
      // The exact string the server checks for -- not "starts with", not
      // "contains a charset" -- see the plan's finding 5.
      expect(captured!.headers['Content-Type'], 'application/octet-stream');
      expect(captured!.headers['Upload-Offset'], '0');
      // Raw bytes, not base64 and not a multipart envelope: the body IS the
      // chunk, byte for byte.
      expect(captured!.bodyBytes, chunk);
    });

    test('threads a non-zero offset through as the header, not the body', () async {
      http.Request? captured;
      final client = MockClient((request) async {
        captured = request;
        return http.Response(
          jsonEncode({
            'status': 'complete',
            'upload_id': 'up-1',
            'byte_size': 108,
            'offset': 108,
            'duplicate': false,
            'memo': {
              'id': 'memo-1',
              'state': 'captured',
              'retention': 'days_30',
              'content_hash': 'abc',
              'byte_size': 108,
              'captured_at': '2026-01-01T00:00:00Z',
              'audio_pruned': false,
              'retention_status': 'scheduled',
              'prunes_at': null,
              'audio_pruned_at': null,
              'duration_ms': null,
              'codec': null,
              'sample_rate_hz': null,
            },
          }),
          200,
        );
      });
      final apiClient = ApiClient(basePath: 'https://chronicle-direct.example.com')
        ..client = client;
      final transport = UploadsApiTransport(UploadsApi(apiClient));

      final state =
          await transport.appendChunk(uploadId: 'up-1', offset: 71, bytes: List.filled(37, 9));

      expect(captured!.headers['Upload-Offset'], '71');
      expect(state.status, UploadStateStatusEnum.complete);
      expect(state.memo!.id, 'memo-1');
    });
  });
}
