/// The gate's response table, row by row (CHRN-120 criterion 1).
///
/// **What this proves is the CONDITION, not the deletion mechanics**: of every
/// shape `GET /audio/{id}` can answer, exactly one -- `410` with code
/// `audio_pruned` -- yields [PruneEligible], and every other shape, including
/// the ones that merely look like it, yields a refusal. `prune_test.dart` then
/// proves a refusal changes nothing on disk.
library;

import 'dart:async';
import 'dart:io';

import 'package:chronicle/api/transport.dart';
import 'package:chronicle/queue/audio_gate_transport.dart';
import 'package:chronicle/queue/failure.dart';
import 'package:chronicle/queue/prune_gate.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;

import 'support/fake_audio_gate.dart';

void main() {
  group('the one shape that permits a delete', () {
    test('410 with code audio_pruned is eligible', () {
      expect(classifyAudioProbe(Answers.pruned), isA<PruneEligible>());
      expect(PruneEligible.code, 'audio_pruned');
    });
  });

  group('every other answer is a refusal', () {
    // (label, response, expected reason, warn, endsPass)
    final refusals = <(String, AudioProbeResponse, String, bool, bool)>[
      ('206: a ranged request honoured, the audio is still there',
          const AudioProbeResponse(206), 'not_yet_pruned', false, false),
      ('bare 200: Range not honoured, the audio is still there',
          const AudioProbeResponse(200), 'range_not_honoured', false, false),
      ('400', const AudioProbeResponse(400, '{"code":"bad_request","message":"m"}'),
          'bad_request', false, false),
      ('404 not_found', Answers.error(404, 'not_found'), 'not_found', true, false),
      ('404 with a non-JSON body', const AudioProbeResponse(404, '<html>nope</html>'),
          'not_found', true, false),
      // The server's row expects audio its file no longer has: not an ordinary
      // outage, so it neither ends the pass nor reads as "try again later" --
      // it is kept and shouted about, because this device may now hold the
      // ONLY copy.
      ('500 audio_missing: the server lost its own copy',
          Answers.error(500, 'audio_missing'), 'audio_missing', true, false),
      ('500 with any other code', Answers.error(500, 'internal'), 'server_error_500',
          false, true),
      ('502', const AudioProbeResponse(502), 'server_error_502', false, true),
      ('503', const AudioProbeResponse(503), 'throttled_503', false, true),
      ('429', const AudioProbeResponse(429), 'throttled_429', false, true),
      ('410 with a different code', Answers.error(410, 'note_withdrawn'),
          'unrecognised_410', true, false),
      ('410 with no body', const AudioProbeResponse(410), 'unrecognised_410', true, false),
      ('410 with a non-JSON body',
          const AudioProbeResponse(410, '<html>gone</html>'), 'unrecognised_410',
          true, false),
      ('410 with JSON that has no code', const AudioProbeResponse(410, '{"message":"m"}'),
          'unrecognised_410', true, false),
      ('410 whose code is not a string',
          const AudioProbeResponse(410, '{"code":410,"message":"m"}'),
          'unrecognised_410', true, false),
      ('403', Answers.error(403, 'forbidden'), 'unexpected_403', true, false),
      ('304 (a validator was never sent)', const AudioProbeResponse(304),
          'unexpected_304', true, false),
      ('204', const AudioProbeResponse(204), 'unexpected_204', true, false),
      ('302 (a redirect is never Chronicle\'s answer)', const AudioProbeResponse(302),
          'unexpected_302', true, false),
    ];

    for (final (label, response, reason, warn, endsPass) in refusals) {
      test(label, () {
        final verdict = classifyAudioProbe(response);
        expect(verdict, isNot(isA<PruneEligible>()), reason: 'never a delete');
        expect(verdict, isA<PruneKeep>());
        final keep = verdict as PruneKeep;
        expect(keep.reason, reason);
        expect(keep.warn, warn);
        expect(keep.endsPass, endsPass);
      });
    }
  });

  group('no answer about this memo ends the pass', () {
    test('401 is a signed-out end, never a delete', () {
      final verdict = classifyAudioProbe(const AudioProbeResponse(401));
      expect(verdict, isA<PruneEnds>());
      expect((verdict as PruneEnds).reason, PassEndReason.signedOut);
    });

    test('a socket error is a network end', () {
      final ends = classifyProbeFailure(const SocketException('no route'));
      expect(ends.reason, PassEndReason.network);
    });

    test('a client exception and a timeout are network ends', () {
      expect(classifyProbeFailure(http.ClientException('reset')).reason,
          PassEndReason.network);
      expect(classifyProbeFailure(TimeoutException('slow')).reason,
          PassEndReason.network);
    });

    test('NotChronicleException is a wrong-host end', () {
      final ends = classifyProbeFailure(NotChronicleException(
        statusCode: 302,
        location: 'https://team.cloudflareaccess.com/login',
        requestedUrl: Uri.parse('https://chronicle.example.com/audio/x'),
      ));
      expect(ends.reason, PassEndReason.wrongHost);
    });

    test('a shape nothing recognises is "no answer", never a delete', () {
      expect(classifyProbeFailure(StateError('who knows')).reason, PassEndReason.network);
    });
  });
}
