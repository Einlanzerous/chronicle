//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ClearedField {
  /// Returns a new [ClearedField] instance.
  ClearedField({
    required this.field,
    required this.value,
    required this.reason,
  });

  String field;

  /// What was proposed, kept so a person can see what was removed.
  String value;

  String reason;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ClearedField &&
    other.field == field &&
    other.value == value &&
    other.reason == reason;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (field.hashCode) +
    (value.hashCode) +
    (reason.hashCode);

  @override
  String toString() => 'ClearedField[field=$field, value=$value, reason=$reason]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'field'] = this.field;
      json[r'value'] = this.value;
      json[r'reason'] = this.reason;
    return json;
  }

  /// Returns a new [ClearedField] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ClearedField? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'field'), 'Required key "ClearedField[field]" is missing from JSON.');
        assert(json[r'field'] != null, 'Required key "ClearedField[field]" has a null value in JSON.');
        assert(json.containsKey(r'value'), 'Required key "ClearedField[value]" is missing from JSON.');
        assert(json[r'value'] != null, 'Required key "ClearedField[value]" has a null value in JSON.');
        assert(json.containsKey(r'reason'), 'Required key "ClearedField[reason]" is missing from JSON.');
        assert(json[r'reason'] != null, 'Required key "ClearedField[reason]" has a null value in JSON.');
        return true;
      }());

      return ClearedField(
        field: mapValueOfType<String>(json, r'field')!,
        value: mapValueOfType<String>(json, r'value')!,
        reason: mapValueOfType<String>(json, r'reason')!,
      );
    }
    return null;
  }

  static List<ClearedField> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ClearedField>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ClearedField.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ClearedField> mapFromJson(dynamic json) {
    final map = <String, ClearedField>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ClearedField.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ClearedField-objects as value to a dart map
  static Map<String, List<ClearedField>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ClearedField>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ClearedField.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'field',
    'value',
    'reason',
  };
}

