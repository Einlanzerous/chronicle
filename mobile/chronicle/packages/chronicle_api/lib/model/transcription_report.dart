//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TranscriptionReport {
  /// Returns a new [TranscriptionReport] instance.
  TranscriptionReport({
    this.states = const {},
    required this.pending,
    required this.held,
    this.heldSample = const [],
    required this.partial,
    this.partialSample = const [],
    required this.enabled,
  });

  /// Every memo counted by state, not only the interesting ones. A report naming only `held` could not tell \"nothing is wrong\" from \"nothing is happening\", and a pump that has silently stopped looks exactly like a corpus with no failures. 
  Map<String, int> states;

  /// Captured, queued and transcribing together. One number, because \"how far behind is it\" is the question being asked. 
  int pending;

  int held;

  List<HeldMemo> heldSample;

  /// Memos whose only transcript is incomplete. Reported because they are otherwise INVISIBLE: a partial memo is `transcribed`, so nothing sweeps it; it is not `held`, so `chronicle retranscribe` will not release it; and its audio correctly does not prune. Every one of those is right, and together they make a partial memo read as a healthy one. 
  int partial;

  List<PartialTranscript> partialSample;

  /// Whether a transcription pump is configured at all. Without it, an operator reading `pending: 812` cannot tell a backlog from a service that was never pointed at an ASR endpoint — and those want completely different remedies. 
  bool enabled;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TranscriptionReport &&
    _deepEquality.equals(other.states, states) &&
    other.pending == pending &&
    other.held == held &&
    _deepEquality.equals(other.heldSample, heldSample) &&
    other.partial == partial &&
    _deepEquality.equals(other.partialSample, partialSample) &&
    other.enabled == enabled;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (states.hashCode) +
    (pending.hashCode) +
    (held.hashCode) +
    (heldSample.hashCode) +
    (partial.hashCode) +
    (partialSample.hashCode) +
    (enabled.hashCode);

  @override
  String toString() => 'TranscriptionReport[states=$states, pending=$pending, held=$held, heldSample=$heldSample, partial=$partial, partialSample=$partialSample, enabled=$enabled]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'states'] = this.states;
      json[r'pending'] = this.pending;
      json[r'held'] = this.held;
      json[r'held_sample'] = this.heldSample;
      json[r'partial'] = this.partial;
      json[r'partial_sample'] = this.partialSample;
      json[r'enabled'] = this.enabled;
    return json;
  }

  /// Returns a new [TranscriptionReport] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TranscriptionReport? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'states'), 'Required key "TranscriptionReport[states]" is missing from JSON.');
        assert(json[r'states'] != null, 'Required key "TranscriptionReport[states]" has a null value in JSON.');
        assert(json.containsKey(r'pending'), 'Required key "TranscriptionReport[pending]" is missing from JSON.');
        assert(json[r'pending'] != null, 'Required key "TranscriptionReport[pending]" has a null value in JSON.');
        assert(json.containsKey(r'held'), 'Required key "TranscriptionReport[held]" is missing from JSON.');
        assert(json[r'held'] != null, 'Required key "TranscriptionReport[held]" has a null value in JSON.');
        assert(json.containsKey(r'partial'), 'Required key "TranscriptionReport[partial]" is missing from JSON.');
        assert(json[r'partial'] != null, 'Required key "TranscriptionReport[partial]" has a null value in JSON.');
        assert(json.containsKey(r'enabled'), 'Required key "TranscriptionReport[enabled]" is missing from JSON.');
        assert(json[r'enabled'] != null, 'Required key "TranscriptionReport[enabled]" has a null value in JSON.');
        return true;
      }());

      return TranscriptionReport(
        states: mapCastOfType<String, int>(json, r'states')!,
        pending: mapValueOfType<int>(json, r'pending')!,
        held: mapValueOfType<int>(json, r'held')!,
        heldSample: HeldMemo.listFromJson(json[r'held_sample']),
        partial: mapValueOfType<int>(json, r'partial')!,
        partialSample: PartialTranscript.listFromJson(json[r'partial_sample']),
        enabled: mapValueOfType<bool>(json, r'enabled')!,
      );
    }
    return null;
  }

  static List<TranscriptionReport> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TranscriptionReport>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TranscriptionReport.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TranscriptionReport> mapFromJson(dynamic json) {
    final map = <String, TranscriptionReport>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TranscriptionReport.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TranscriptionReport-objects as value to a dart map
  static Map<String, List<TranscriptionReport>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TranscriptionReport>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TranscriptionReport.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'states',
    'pending',
    'held',
    'partial',
    'enabled',
  };
}

