/// The prune pass's one question to the server, and nothing else: "is your copy
/// of this memo's audio still there?" -- answered by `GET /audio/{memo_id}`
/// asked for a single byte.
///
/// Shaped after the generated `MemosApi`, the same thin way
/// `uploads_api_transport.dart` wraps `UploadsApi`, so a fake can stand in for
/// it exactly and the gate's decisions (`prune_gate.dart`) are tested without
/// a socket.
///
/// **Why this returns a raw response instead of throwing on a bad status.**
/// The generated `getMemoAudio` throws `ApiException` on any status >= 400 and
/// discards the response, and here the status IS the answer: the signal that
/// permits a delete is `410`, itself a non-2xx. `getMemoAudioWithHttpInfo`
/// hands back the `Response` untouched, so this seam reports a status and a
/// body and decides nothing. Only a failure to get any HTTP answer at all --
/// a socket, TLS or timeout error, or a host that is not Chronicle -- throws,
/// and it throws the same shapes `classifyError` already understands.
library;

import 'package:chronicle_api/api.dart';

/// What one probe got back. [body] is populated only for a status >= 400,
/// where the server's `{code, message}` envelope lives; a success carries
/// audio, and this never reads it.
class AudioProbeResponse {
  const AudioProbeResponse(this.statusCode, [this.body]);

  final int statusCode;
  final String? body;
}

abstract class AudioGateTransport {
  /// `GET /audio/{memoId}` with `Range: bytes=0-0`.
  ///
  /// Throws only when no usable HTTP answer arrived. Any answer, whatever its
  /// status, is returned.
  Future<AudioProbeResponse> probe(String memoId);
}

class MemosApiAudioGateTransport implements AudioGateTransport {
  MemosApiAudioGateTransport(this._api);

  final MemosApi _api;

  /// One byte, not the recording. `http.ServeContent` answers a satisfiable
  /// `Range` with `206` and that byte, so a memo whose audio still exists costs
  /// one byte on the wire rather than its whole length.
  ///
  /// **A proxy that strips `Range` turns this into a full download**: the
  /// generated client reads the whole body before returning. That is bounded --
  /// one probe per capture per 24 hours (`prune.dart`) -- and `prune_gate.dart`
  /// reports the case so it is visible rather than silent.
  @override
  Future<AudioProbeResponse> probe(String memoId) async {
    final response = await _api.getMemoAudioWithHttpInfo(memoId, range: 'bytes=0-0');
    return AudioProbeResponse(
      response.statusCode,
      response.statusCode >= 400 ? response.body : null,
    );
  }
}
