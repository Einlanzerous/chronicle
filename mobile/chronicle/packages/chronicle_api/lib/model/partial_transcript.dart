//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class PartialTranscript {
  /// Returns a new [PartialTranscript] instance.
  PartialTranscript({
    required this.memoId,
    required this.model,
    required this.transcribedAt,
  });

  String memoId;

  /// Runner-qualified, e.g. `whisper.cpp/small.en`.
  String model;

  DateTime transcribedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is PartialTranscript &&
    other.memoId == memoId &&
    other.model == model &&
    other.transcribedAt == transcribedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode) +
    (model.hashCode) +
    (transcribedAt.hashCode);

  @override
  String toString() => 'PartialTranscript[memoId=$memoId, model=$model, transcribedAt=$transcribedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
      json[r'model'] = this.model;
      json[r'transcribed_at'] = this.transcribedAt.toUtc().toIso8601String();
    return json;
  }

  /// Returns a new [PartialTranscript] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static PartialTranscript? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "PartialTranscript[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "PartialTranscript[memo_id]" has a null value in JSON.');
        assert(json.containsKey(r'model'), 'Required key "PartialTranscript[model]" is missing from JSON.');
        assert(json[r'model'] != null, 'Required key "PartialTranscript[model]" has a null value in JSON.');
        assert(json.containsKey(r'transcribed_at'), 'Required key "PartialTranscript[transcribed_at]" is missing from JSON.');
        assert(json[r'transcribed_at'] != null, 'Required key "PartialTranscript[transcribed_at]" has a null value in JSON.');
        return true;
      }());

      return PartialTranscript(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        model: mapValueOfType<String>(json, r'model')!,
        transcribedAt: mapDateTime(json, r'transcribed_at', r'')!,
      );
    }
    return null;
  }

  static List<PartialTranscript> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <PartialTranscript>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = PartialTranscript.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, PartialTranscript> mapFromJson(dynamic json) {
    final map = <String, PartialTranscript>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = PartialTranscript.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of PartialTranscript-objects as value to a dart map
  static Map<String, List<PartialTranscript>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<PartialTranscript>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = PartialTranscript.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
    'model',
    'transcribed_at',
  };
}

