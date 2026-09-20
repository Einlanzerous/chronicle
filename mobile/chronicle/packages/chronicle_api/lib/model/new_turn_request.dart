//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class NewTurnRequest {
  /// Returns a new [NewTurnRequest] instance.
  NewTurnRequest({
    required this.body,
    this.composedAt,
  });

  String body;

  /// When the client says it was written. Carried on the turn; orders nothing.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? composedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is NewTurnRequest &&
    other.body == body &&
    other.composedAt == composedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (body.hashCode) +
    (composedAt == null ? 0 : composedAt!.hashCode);

  @override
  String toString() => 'NewTurnRequest[body=$body, composedAt=$composedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'body'] = this.body;
    if (this.composedAt != null) {
      json[r'composed_at'] = this.composedAt!.toUtc().toIso8601String();
    } else {
      json[r'composed_at'] = null;
    }
    return json;
  }

  /// Returns a new [NewTurnRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static NewTurnRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'body'), 'Required key "NewTurnRequest[body]" is missing from JSON.');
        assert(json[r'body'] != null, 'Required key "NewTurnRequest[body]" has a null value in JSON.');
        return true;
      }());

      return NewTurnRequest(
        body: mapValueOfType<String>(json, r'body')!,
        composedAt: mapDateTime(json, r'composed_at', r''),
      );
    }
    return null;
  }

  static List<NewTurnRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <NewTurnRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = NewTurnRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, NewTurnRequest> mapFromJson(dynamic json) {
    final map = <String, NewTurnRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = NewTurnRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of NewTurnRequest-objects as value to a dart map
  static Map<String, List<NewTurnRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<NewTurnRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = NewTurnRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'body',
  };
}

