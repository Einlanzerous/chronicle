/// Classifies one attempt's outcome. This is the one place `ApiException`,
/// its `innerException`, and Chronicle's own JSON error envelope get turned
/// into a decision -- everything else in the queue trusts this module's
/// answer and never re-inspects an exception or a status code on its own.
///
/// **Scope.** This module answers "what kind of thing just happened", pure
/// and stateless. It does not decide when a repeated [CaptureFailure]
/// becomes [QueueStatus.rejected], or when a [FailureClass.transientServer]
/// streak becomes a `NOT SENT — server error` label -- those need the
/// capture's persisted history and, for the label, evidence that OTHER
/// captures are succeeding, which belongs in the engine that drives this
/// classifier against real I/O, not in a pure function.
library;

import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:chronicle_api/api.dart';
import 'package:http/http.dart' as http;

import '../api/transport.dart';
import 'queue_record.dart';

/// What the engine should do next, and why.
sealed class Outcome {
  const Outcome();
}

/// Re-open (or continue) from [offset]. An ordinary event, not a failure --
/// `incomplete`, a `408`, and an offset-`409` all mean this.
class Resume extends Outcome {
  const Resume(this.offset);
  final int offset;
}

/// Ends the whole pass: nothing else will work right now either.
///
/// [captureFailureClass] is set only when this capture itself should record
/// the failure (a network-class error, backed off like any other capture
/// failure). A device block (401, an Access-gated host) records nothing
/// against the capture that happened to surface it -- these are facts about
/// the device, not about that capture; see `device_block.dart`.
class PassEnds extends Outcome {
  const PassEnds(this.reason, {this.captureFailureClass});
  final PassEndReason reason;
  final FailureClass? captureFailureClass;
}

enum PassEndReason { network, wrongHost, signedOut }

/// A failure specific to this one capture. The pass continues to the next
/// capture.
class CaptureFailure extends Outcome {
  const CaptureFailure(this.failureClass, {this.code, this.parkImmediately = false});

  final FailureClass failureClass;
  final String? code;

  /// True for 409 `idempotency_key_reused` and 422 `content_hash_mismatch`:
  /// parked on the FIRST occurrence, never retried automatically, because
  /// no number of retries changes a fact about the content.
  final bool parkImmediately;
}

/// A 2xx response that does not verify as an ack (see `ack.dart`). Treated
/// exactly like "no answer": the safe default for an outcome this ambiguous
/// is to try again, never to conclude the memo was sent.
const Outcome unverifiedAck = CaptureFailure(FailureClass.network, code: 'unverified_2xx');

/// Classifies a raised error -- reached before any HTTP response was usable,
/// or thrown by the generated client's own `>= 400` check.
///
/// Handles BOTH shapes the generated client can produce: `ApiException`
/// with `innerException` set (the documented wrap, `api_client.dart:109-142`),
/// and the raw exception itself, which reaches the caller un-wrapped when
/// the failure happens while `invokeAPI` reads the response body (`return
/// Response.fromStream(response)` with no `await`, at lines 78, 88 and 108
/// -- confirmed against the generated source, not hypothetical). Both must
/// classify identically, because which shape a given failure arrives in
/// depends on exactly when the connection died, not on anything the queue
/// controls.
///
/// [sentFromOffset] is the offset the chunk that failed was sent from, if
/// this was a chunk PATCH; null for the initial POST. It is only used to
/// judge progress on a resume, so passing it here costs nothing when the
/// failure turns out not to be one.
Outcome classifyError(Object error, {int? sentFromOffset}) {
  final unwrapped =
      error is ApiException && error.innerException != null
          ? error.innerException!
          : error;

  if (unwrapped is NotChronicleException) {
    return const PassEnds(PassEndReason.wrongHost);
  }
  if (_isNetworkError(unwrapped)) {
    return const PassEnds(PassEndReason.network,
        captureFailureClass: FailureClass.network);
  }
  if (error is ApiException) {
    return classifyResponse(error.code, error.message,
        sentFromOffset: sentFromOffset);
  }
  // Anything left is unrecognised. Treated as "no answer", the same safe
  // default as [unverifiedAck]: never a rejection, always a retry.
  return const PassEnds(PassEndReason.network,
      captureFailureClass: FailureClass.network);
}

bool _isNetworkError(Object error) =>
    error is SocketException ||
    error is HttpException ||
    error is TlsException ||
    error is http.ClientException ||
    error is TimeoutException ||
    error is FormatException;

/// Classifies an HTTP answer that WAS received: a status code and its raw
/// body, exactly what `ApiException(code, message)` carries once the
/// generated client's own `>= 400` check fires.
///
/// **Keyed on (status, body `code`), not on status alone**, because the
/// three 409 shapes are three different instructions: an offset conflict
/// carries an `UploadState` body, `idempotency_key_reused` carries
/// Chronicle's error envelope, and `ErrStagingLost` is a 409 at offset 0 --
/// also an `UploadState` body, meaning "resend from zero". The generated
/// `appendChunk`/`openUpload` throw `ApiException(code, bodyString)` and
/// drop the `Upload-Offset` header entirely, so the body is the only
/// discriminator available at this layer -- the reason "resume by
/// re-opening" is the queue's uniform answer rather than trying to read an
/// offset out of an error.
Outcome classifyResponse(int status, String? body, {int? sentFromOffset}) {
  if (status == 401) return const PassEnds(PassEndReason.signedOut);

  final decoded = _tryDecodeMap(body);

  // An UploadState-shaped body -- it carries an integer `offset` -- means
  // "resume", whatever the status text says: the server's own transfer-cut
  // (408) and offset-conflict (409) answers are both this shape.
  final offset = decoded?['offset'];
  if (offset is int) {
    return _resumeOrNoProgress(offset, sentFromOffset);
  }

  // A 408 or 429 WITHOUT that shape -- a proxy's own timeout or rate limit,
  // carrying no offset -- is still transient: it is not a fact about this
  // capture's content, so it must never be parked.
  if (status == 408 || status == 429) {
    return const CaptureFailure(FailureClass.transientServer);
  }

  final errorCode = decoded?['code'] as String?;
  if (status == 409 && errorCode == 'idempotency_key_reused') {
    return const CaptureFailure(
      FailureClass.rejectedKeyReused,
      code: 'idempotency_key_reused',
      parkImmediately: true,
    );
  }
  if (status == 422 && errorCode == 'content_hash_mismatch') {
    return const CaptureFailure(
      FailureClass.rejectedHashMismatch,
      code: 'content_hash_mismatch',
      parkImmediately: true,
    );
  }
  if (status == 422 && errorCode == 'oversend') {
    return const CaptureFailure(FailureClass.protocolOversend, code: 'oversend');
  }
  if (status >= 500 && status < 600) {
    return CaptureFailure(FailureClass.transientServer, code: '$status');
  }
  if (status >= 400) {
    return CaptureFailure(FailureClass.rejectedOther, code: '$status');
  }
  // A status under 400 reaching this function is not something the
  // generated client throws as an ApiException; nothing calls this path
  // with one. Treated the same as any other unrecognised shape: no answer.
  return const CaptureFailure(FailureClass.network);
}

/// The progress guard. A resume counts as progress only when the server's
/// stated [serverOffset] is higher than the offset the chunk was sent from.
/// Without this, a repeating `ErrStagingLost` (409 at offset 0,
/// `internal/upload/upload.go:413`) would have the client re-send the whole
/// file and hear the same answer forever, with no backoff -- a hot loop
/// rather than a retry. [sentFromOffset] is null for the initial POST,
/// which by construction always "progresses" from an unknown baseline.
Outcome _resumeOrNoProgress(int serverOffset, int? sentFromOffset) {
  if (sentFromOffset != null && serverOffset <= sentFromOffset) {
    return const CaptureFailure(FailureClass.noProgress);
  }
  return Resume(serverOffset);
}

Map<String, Object?>? _tryDecodeMap(String? body) {
  if (body == null || body.isEmpty) return null;
  try {
    final decoded = jsonDecode(body);
    return decoded is Map ? decoded.cast<String, Object?>() : null;
  } catch (_) {
    return null;
  }
}
