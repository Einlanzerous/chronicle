//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ProvenanceTranscript {
  /// Returns a new [ProvenanceTranscript] instance.
  ProvenanceTranscript({
    required this.present,
    required this.readable,
    this.model,
    this.transcribedAt,
    this.partial,
  });

  /// A transcript row exists. Says nothing about whether this caller may read it.
  bool present;

  /// This caller may ask `getMemoTranscript` for the words: the memo's author, or the owner. 
  bool readable;

  /// Runner-qualified, as the store holds it — `whisper.cpp/small.en`, not `small.en`. Absent when no transcript exists. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? model;

  /// Absent when no transcript exists.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? transcribedAt;

  /// The ASR service recorded that its own run did not complete, and this row is the fallback `GetTranscript` returned because no complete transcript exists yet. A non-author holds `readable: false` and has no other way to learn that a `present: true` memo has only an incomplete transcript behind it. Never computed here, and never from `covered_ms` against a duration. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  bool? partial;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ProvenanceTranscript &&
    other.present == present &&
    other.readable == readable &&
    other.model == model &&
    other.transcribedAt == transcribedAt &&
    other.partial == partial;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (present.hashCode) +
    (readable.hashCode) +
    (model == null ? 0 : model!.hashCode) +
    (transcribedAt == null ? 0 : transcribedAt!.hashCode) +
    (partial == null ? 0 : partial!.hashCode);

  @override
  String toString() => 'ProvenanceTranscript[present=$present, readable=$readable, model=$model, transcribedAt=$transcribedAt, partial=$partial]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'present'] = this.present;
      json[r'readable'] = this.readable;
    if (this.model != null) {
      json[r'model'] = this.model;
    } else {
      json[r'model'] = null;
    }
    if (this.transcribedAt != null) {
      json[r'transcribed_at'] = this.transcribedAt!.toUtc().toIso8601String();
    } else {
      json[r'transcribed_at'] = null;
    }
    if (this.partial != null) {
      json[r'partial'] = this.partial;
    } else {
      json[r'partial'] = null;
    }
    return json;
  }

  /// Returns a new [ProvenanceTranscript] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ProvenanceTranscript? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'present'), 'Required key "ProvenanceTranscript[present]" is missing from JSON.');
        assert(json[r'present'] != null, 'Required key "ProvenanceTranscript[present]" has a null value in JSON.');
        assert(json.containsKey(r'readable'), 'Required key "ProvenanceTranscript[readable]" is missing from JSON.');
        assert(json[r'readable'] != null, 'Required key "ProvenanceTranscript[readable]" has a null value in JSON.');
        return true;
      }());

      return ProvenanceTranscript(
        present: mapValueOfType<bool>(json, r'present')!,
        readable: mapValueOfType<bool>(json, r'readable')!,
        model: mapValueOfType<String>(json, r'model'),
        transcribedAt: mapDateTime(json, r'transcribed_at', r''),
        partial: mapValueOfType<bool>(json, r'partial'),
      );
    }
    return null;
  }

  static List<ProvenanceTranscript> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ProvenanceTranscript>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ProvenanceTranscript.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ProvenanceTranscript> mapFromJson(dynamic json) {
    final map = <String, ProvenanceTranscript>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ProvenanceTranscript.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ProvenanceTranscript-objects as value to a dart map
  static Map<String, List<ProvenanceTranscript>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ProvenanceTranscript>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ProvenanceTranscript.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'present',
    'readable',
  };
}

