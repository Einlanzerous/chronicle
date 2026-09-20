//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TranscriptSegment {
  /// Returns a new [TranscriptSegment] instance.
  TranscriptSegment({
    required this.startMs,
    required this.endMs,
    required this.text,
  });

  int startMs;

  int endMs;

  String text;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TranscriptSegment &&
    other.startMs == startMs &&
    other.endMs == endMs &&
    other.text == text;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (startMs.hashCode) +
    (endMs.hashCode) +
    (text.hashCode);

  @override
  String toString() => 'TranscriptSegment[startMs=$startMs, endMs=$endMs, text=$text]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'start_ms'] = this.startMs;
      json[r'end_ms'] = this.endMs;
      json[r'text'] = this.text;
    return json;
  }

  /// Returns a new [TranscriptSegment] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TranscriptSegment? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'start_ms'), 'Required key "TranscriptSegment[start_ms]" is missing from JSON.');
        assert(json[r'start_ms'] != null, 'Required key "TranscriptSegment[start_ms]" has a null value in JSON.');
        assert(json.containsKey(r'end_ms'), 'Required key "TranscriptSegment[end_ms]" is missing from JSON.');
        assert(json[r'end_ms'] != null, 'Required key "TranscriptSegment[end_ms]" has a null value in JSON.');
        assert(json.containsKey(r'text'), 'Required key "TranscriptSegment[text]" is missing from JSON.');
        assert(json[r'text'] != null, 'Required key "TranscriptSegment[text]" has a null value in JSON.');
        return true;
      }());

      return TranscriptSegment(
        startMs: mapValueOfType<int>(json, r'start_ms')!,
        endMs: mapValueOfType<int>(json, r'end_ms')!,
        text: mapValueOfType<String>(json, r'text')!,
      );
    }
    return null;
  }

  static List<TranscriptSegment> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TranscriptSegment>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TranscriptSegment.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TranscriptSegment> mapFromJson(dynamic json) {
    final map = <String, TranscriptSegment>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TranscriptSegment.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TranscriptSegment-objects as value to a dart map
  static Map<String, List<TranscriptSegment>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TranscriptSegment>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TranscriptSegment.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'start_ms',
    'end_ms',
    'text',
  };
}

