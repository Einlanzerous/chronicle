import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:chronicle/api/transport.dart';
import 'package:chronicle/queue/failure.dart';
import 'package:chronicle/queue/queue_record.dart';
import 'package:chronicle_api/api.dart';
import 'package:http/http.dart' as http;
import 'package:flutter_test/flutter_test.dart';

/// Every network-class error the generated client's wrap can produce,
/// keyed by name so a failing case is legible.
final _networkErrors = <String, Object>{
  'SocketException': const SocketException('connection refused'),
  'HttpException': const HttpException('bad'),
  'TlsException': const TlsException('handshake failed'),
  'http.ClientException': http.ClientException('closed'),
  'TimeoutException': TimeoutException('deadline'),
  'FormatException (a captive-portal HTML 200)': const FormatException('not JSON'),
};

void main() {
  group('classifyError: no HTTP answer -- wrapped shape', () {
    for (final entry in _networkErrors.entries) {
      test('${entry.key}, wrapped in ApiException(400, innerException:), '
          'ends the pass and is never a rejection', () {
        final wrapped = ApiException.withInner(
          400,
          'Exception occurred: PATCH /memos/uploads/x',
          entry.value as Exception,
          StackTrace.empty,
        );
        final outcome = classifyError(wrapped);
        expect(outcome, isA<PassEnds>());
        final ends = outcome as PassEnds;
        expect(ends.reason, PassEndReason.network);
        expect(ends.captureFailureClass, FailureClass.network);
      });
    }
  });

  group('classifyError: no HTTP answer -- raw (unwrapped) shape', () {
    // Confirmed against the generated client: invokeAPI's
    // `return Response.fromStream(response);` is not awaited inside its own
    // try/catch, so a failure while reading the response body escapes those
    // clauses and reaches the caller as the raw exception, not as an
    // ApiException. This is exactly the ambiguous case this ticket exists
    // for -- a connection cut after the last byte, before the answer -- so
    // it must classify identically to the wrapped shape.
    for (final entry in _networkErrors.entries) {
      test('raw ${entry.key} classifies the same as its wrapped form', () {
        final outcome = classifyError(entry.value);
        expect(outcome, isA<PassEnds>());
        final ends = outcome as PassEnds;
        expect(ends.reason, PassEndReason.network);
        expect(ends.captureFailureClass, FailureClass.network);
      });
    }
  });

  test('NotChronicleException is a wrongHost pass-end, never a rejection', () {
    final outcome = classifyError(NotChronicleException(
      statusCode: 302,
      location: 'https://chronicle.example.com/cdn-cgi/access/login',
      requestedUrl: Uri.parse('https://chronicle.example.com/memos/uploads'),
    ));
    expect(outcome, isA<PassEnds>());
    expect((outcome as PassEnds).reason, PassEndReason.wrongHost);
    // A device-level block records nothing against the capture itself.
    expect(outcome.captureFailureClass, isNull);
  });

  test('an entirely unrecognised thrown object is treated as no answer, not a rejection', () {
    final outcome = classifyError(StateError('something the queue has never seen'));
    expect(outcome, isA<PassEnds>());
    expect((outcome as PassEnds).reason, PassEndReason.network);
  });

  group('classifyResponse: keyed on (status, body code), not on status alone', () {
    test('401 is a signedOut pass-end', () {
      final outcome = classifyResponse(401, jsonEncode({'code': 'unauthorized'}));
      expect(outcome, isA<PassEnds>());
      expect((outcome as PassEnds).reason, PassEndReason.signedOut);
      expect(outcome.captureFailureClass, isNull);
    });

    test('a 200/201-shaped incomplete offset resumes -- not exercised here '
        '(handled directly by the engine from a parsed UploadState), but a '
        '409 carrying the same UploadState shape resumes identically', () {
      final body = jsonEncode({'status': 'incomplete', 'offset': 4096, 'byte_size': 8192, 'duplicate': false});
      final outcome = classifyResponse(409, body, sentFromOffset: 0);
      expect(outcome, isA<Resume>());
      expect((outcome as Resume).offset, 4096);
    });

    test('409 offset conflict at offset 0 (ErrStagingLost) resumes from zero', () {
      final body = jsonEncode({'status': 'incomplete', 'offset': 0, 'byte_size': 8192, 'duplicate': false});
      final outcome = classifyResponse(409, body, sentFromOffset: 100);
      // 0 <= sentFromOffset(100): this is the no-progress case, not a plain resume.
      expect(outcome, isA<CaptureFailure>());
      expect((outcome as CaptureFailure).failureClass, FailureClass.noProgress);
    });

    test('a genuine offset resume with no prior offset (the initial POST) always progresses', () {
      final body = jsonEncode({'status': 'incomplete', 'offset': 0, 'byte_size': 8192, 'duplicate': false});
      final outcome = classifyResponse(200, body, sentFromOffset: null);
      expect(outcome, isA<Resume>());
      expect((outcome as Resume).offset, 0);
    });

    test('a server 408 (UploadState body) resumes from the stated offset', () {
      final body = jsonEncode({'status': 'incomplete', 'offset': 2048, 'byte_size': 8192, 'duplicate': false});
      final outcome = classifyResponse(408, body, sentFromOffset: 0);
      expect(outcome, isA<Resume>());
      expect((outcome as Resume).offset, 2048);
    });

    test('a proxy 408 with no UploadState body is transient, never parked', () {
      final outcome = classifyResponse(408, 'Request Timeout');
      expect(outcome, isA<CaptureFailure>());
      expect((outcome as CaptureFailure).failureClass, FailureClass.transientServer);
      expect(outcome.parkImmediately, isFalse);
    });

    test('a bare 429 (too_many_open_uploads, or a proxy rate limit) is transient', () {
      final outcome = classifyResponse(429, jsonEncode({'code': 'too_many_open_uploads'}));
      expect(outcome, isA<CaptureFailure>());
      expect((outcome as CaptureFailure).failureClass, FailureClass.transientServer);
    });

    test('409 idempotency_key_reused is rejected immediately, on the first occurrence', () {
      final outcome = classifyResponse(409, jsonEncode({'code': 'idempotency_key_reused'}));
      expect(outcome, isA<CaptureFailure>());
      final failure = outcome as CaptureFailure;
      expect(failure.failureClass, FailureClass.rejectedKeyReused);
      expect(failure.parkImmediately, isTrue);
    });

    test('422 content_hash_mismatch is rejected immediately, on the first occurrence', () {
      final outcome = classifyResponse(422, jsonEncode({'code': 'content_hash_mismatch'}));
      expect(outcome, isA<CaptureFailure>());
      final failure = outcome as CaptureFailure;
      expect(failure.failureClass, FailureClass.rejectedHashMismatch);
      expect(failure.parkImmediately, isTrue);
    });

    test('422 oversend is a protocol failure, not parked on the first occurrence', () {
      final outcome = classifyResponse(422, jsonEncode({'code': 'oversend'}));
      expect(outcome, isA<CaptureFailure>());
      final failure = outcome as CaptureFailure;
      expect(failure.failureClass, FailureClass.protocolOversend);
      expect(failure.parkImmediately, isFalse);
    });

    for (final status in [500, 502, 503]) {
      test('$status is a transient, capture-specific server failure', () {
        final outcome = classifyResponse(status, null);
        expect(outcome, isA<CaptureFailure>());
        final failure = outcome as CaptureFailure;
        expect(failure.failureClass, FailureClass.transientServer);
        expect(failure.parkImmediately, isFalse);
      });
    }

    for (final status in [400, 404, 411, 413, 415]) {
      test('a real $status answer is rejectedOther, not transient and not parked yet '
          '(the "after 3" escalation is the engine\'s job, not this function\'s)', () {
        final outcome = classifyResponse(status, jsonEncode({'code': 'something_specific'}));
        expect(outcome, isA<CaptureFailure>());
        final failure = outcome as CaptureFailure;
        expect(failure.failureClass, FailureClass.rejectedOther);
        expect(failure.parkImmediately, isFalse);
      });
    }

    test('a body that fails to decode as JSON does not crash classification', () {
      final outcome = classifyResponse(500, 'not json at all {{{');
      expect(outcome, isA<CaptureFailure>());
      expect((outcome as CaptureFailure).failureClass, FailureClass.transientServer);
    });

    test('a null body does not crash classification', () {
      final outcome = classifyResponse(500, null);
      expect(outcome, isA<CaptureFailure>());
    });
  });

  group('classifyError delegates a real ApiException(>=400) to classifyResponse', () {
    test('an ApiException with no innerException carries the real status through', () {
      final outcome = classifyError(
        ApiException(409, jsonEncode({'code': 'idempotency_key_reused'})),
      );
      expect(outcome, isA<CaptureFailure>());
      expect((outcome as CaptureFailure).failureClass, FailureClass.rejectedKeyReused);
    });

    test('sentFromOffset is threaded through to the progress guard', () {
      final body = jsonEncode({'status': 'incomplete', 'offset': 10, 'byte_size': 100, 'duplicate': false});
      final outcome = classifyError(
        ApiException(409, body),
        sentFromOffset: 10,
      );
      expect(outcome, isA<CaptureFailure>());
      expect((outcome as CaptureFailure).failureClass, FailureClass.noProgress);
    });
  });

  test('unverifiedAck is a network-class capture failure, never a rejection', () {
    expect(unverifiedAck, isA<CaptureFailure>());
    final failure = unverifiedAck as CaptureFailure;
    expect(failure.failureClass, FailureClass.network);
    expect(failure.parkImmediately, isFalse);
  });
}
