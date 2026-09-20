//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class MemoTranscript {
  /// Returns a new [MemoTranscript] instance.
  MemoTranscript({
    required this.memoId,
    required this.text,
    this.segments = const [],
    required this.partial,
    required this.model,
    required this.backend,
    required this.audioDurationMs,
    required this.coveredMs,
    required this.transcribedAt,
  });

  String memoId;

  /// Never null and MAY BE EMPTY. A memo that is forty seconds of silence has a true and complete answer, and the answer is \"no speech\". 
  String text;

  /// The spans of speech, in order. Whole rather than bounded: the live corpus averages under two minutes a memo, which is a few kilobytes of segments, so a bounded-or-not ruling here would be a ruling about nothing. 
  List<TranscriptSegment> segments;

  /// The service recorded that its own run did not complete. A partial transcript never satisfies the durability floor, so it never lets the pruner take the audio. 
  bool partial;

  /// Runner-qualified, as the store holds it — `whisper.cpp/small.en`.
  String model;

  /// What ran it — `vulkan`, `cpu`.
  String backend;

  /// Measured off the normalised 16 kHz mono WAV. Evidence, not a predicate. 
  int? audioDurationMs;

  /// How much of the recording the segments span. Short of the duration on any recording that ends in silence, which is most of them — so it is evidence and never a completeness test. 
  int? coveredMs;

  DateTime transcribedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is MemoTranscript &&
    other.memoId == memoId &&
    other.text == text &&
    _deepEquality.equals(other.segments, segments) &&
    other.partial == partial &&
    other.model == model &&
    other.backend == backend &&
    other.audioDurationMs == audioDurationMs &&
    other.coveredMs == coveredMs &&
    other.transcribedAt == transcribedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode) +
    (text.hashCode) +
    (segments.hashCode) +
    (partial.hashCode) +
    (model.hashCode) +
    (backend.hashCode) +
    (audioDurationMs == null ? 0 : audioDurationMs!.hashCode) +
    (coveredMs == null ? 0 : coveredMs!.hashCode) +
    (transcribedAt.hashCode);

  @override
  String toString() => 'MemoTranscript[memoId=$memoId, text=$text, segments=$segments, partial=$partial, model=$model, backend=$backend, audioDurationMs=$audioDurationMs, coveredMs=$coveredMs, transcribedAt=$transcribedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
      json[r'text'] = this.text;
      json[r'segments'] = this.segments;
      json[r'partial'] = this.partial;
      json[r'model'] = this.model;
      json[r'backend'] = this.backend;
    if (this.audioDurationMs != null) {
      json[r'audio_duration_ms'] = this.audioDurationMs;
    } else {
      json[r'audio_duration_ms'] = null;
    }
    if (this.coveredMs != null) {
      json[r'covered_ms'] = this.coveredMs;
    } else {
      json[r'covered_ms'] = null;
    }
      json[r'transcribed_at'] = this.transcribedAt.toUtc().toIso8601String();
    return json;
  }

  /// Returns a new [MemoTranscript] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static MemoTranscript? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "MemoTranscript[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "MemoTranscript[memo_id]" has a null value in JSON.');
        assert(json.containsKey(r'text'), 'Required key "MemoTranscript[text]" is missing from JSON.');
        assert(json[r'text'] != null, 'Required key "MemoTranscript[text]" has a null value in JSON.');
        assert(json.containsKey(r'segments'), 'Required key "MemoTranscript[segments]" is missing from JSON.');
        assert(json[r'segments'] != null, 'Required key "MemoTranscript[segments]" has a null value in JSON.');
        assert(json.containsKey(r'partial'), 'Required key "MemoTranscript[partial]" is missing from JSON.');
        assert(json[r'partial'] != null, 'Required key "MemoTranscript[partial]" has a null value in JSON.');
        assert(json.containsKey(r'model'), 'Required key "MemoTranscript[model]" is missing from JSON.');
        assert(json[r'model'] != null, 'Required key "MemoTranscript[model]" has a null value in JSON.');
        assert(json.containsKey(r'backend'), 'Required key "MemoTranscript[backend]" is missing from JSON.');
        assert(json[r'backend'] != null, 'Required key "MemoTranscript[backend]" has a null value in JSON.');
        assert(json.containsKey(r'audio_duration_ms'), 'Required key "MemoTranscript[audio_duration_ms]" is missing from JSON.');
        assert(json.containsKey(r'covered_ms'), 'Required key "MemoTranscript[covered_ms]" is missing from JSON.');
        assert(json.containsKey(r'transcribed_at'), 'Required key "MemoTranscript[transcribed_at]" is missing from JSON.');
        assert(json[r'transcribed_at'] != null, 'Required key "MemoTranscript[transcribed_at]" has a null value in JSON.');
        return true;
      }());

      return MemoTranscript(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        text: mapValueOfType<String>(json, r'text')!,
        segments: TranscriptSegment.listFromJson(json[r'segments']),
        partial: mapValueOfType<bool>(json, r'partial')!,
        model: mapValueOfType<String>(json, r'model')!,
        backend: mapValueOfType<String>(json, r'backend')!,
        audioDurationMs: mapValueOfType<int>(json, r'audio_duration_ms'),
        coveredMs: mapValueOfType<int>(json, r'covered_ms'),
        transcribedAt: mapDateTime(json, r'transcribed_at', r'')!,
      );
    }
    return null;
  }

  static List<MemoTranscript> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <MemoTranscript>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = MemoTranscript.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, MemoTranscript> mapFromJson(dynamic json) {
    final map = <String, MemoTranscript>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = MemoTranscript.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of MemoTranscript-objects as value to a dart map
  static Map<String, List<MemoTranscript>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<MemoTranscript>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = MemoTranscript.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
    'text',
    'segments',
    'partial',
    'model',
    'backend',
    'audio_duration_ms',
    'covered_ms',
    'transcribed_at',
  };
}

