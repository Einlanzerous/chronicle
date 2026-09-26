/// A scriptable [AudioGateTransport] for the prune pass's tests: what the
/// server says about each memo's audio, with a record of every time it was
/// asked.
///
/// **It records rather than throws when called out of turn.** `prunePass`
/// treats anything a probe throws as "no answer" and carries on, so a
/// `fail(...)` from in here would be swallowed as a network error and the test
/// would pass. Tests that must prove the server was never asked assert
/// [probed] is empty afterwards instead.
library;

import 'dart:async';

import 'package:chronicle/queue/audio_gate_transport.dart';

/// The wire answers `prune_gate.dart`'s table names, as a test writes them.
class Answers {
  static AudioProbeResponse get stillThere => const AudioProbeResponse(206);

  /// The one that permits a delete.
  static AudioProbeResponse get pruned => const AudioProbeResponse(
      410, '{"code":"audio_pruned","message":"this recording was pruned"}');

  static AudioProbeResponse error(int status, String code) =>
      AudioProbeResponse(status, '{"code":"$code","message":"m"}');
}

class FakeAudioGate implements AudioGateTransport {
  FakeAudioGate([Map<String, AudioProbeResponse>? responses])
      : responses = responses ?? {};

  /// What each memo id answers. A memo with no entry answers [fallback].
  final Map<String, AudioProbeResponse> responses;

  AudioProbeResponse fallback = Answers.stillThere;

  /// Every memo id asked about, in order.
  final List<String> probed = [];

  /// Thrown INSTEAD of answering, from the first probe on, when set.
  Object? throwInstead;

  /// Awaited before answering -- a test holds a probe open here to interleave
  /// something else with the prune leg.
  Future<void> Function(String memoId)? onProbe;

  @override
  Future<AudioProbeResponse> probe(String memoId) async {
    probed.add(memoId);
    await onProbe?.call(memoId);
    final error = throwInstead;
    if (error != null) throw error;
    return responses[memoId] ?? fallback;
  }
}
