//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SsoError {
  /// Returns a new [SsoError] instance.
  SsoError({
    required this.code,
    required this.message,
    this.email,
  });

  String code;

  String message;

  /// The verified email the assertion named. Absent when there was no assertion to read.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? email;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SsoError &&
    other.code == code &&
    other.message == message &&
    other.email == email;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (code.hashCode) +
    (message.hashCode) +
    (email == null ? 0 : email!.hashCode);

  @override
  String toString() => 'SsoError[code=$code, message=$message, email=$email]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'code'] = this.code;
      json[r'message'] = this.message;
    if (this.email != null) {
      json[r'email'] = this.email;
    } else {
      json[r'email'] = null;
    }
    return json;
  }

  /// Returns a new [SsoError] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SsoError? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'code'), 'Required key "SsoError[code]" is missing from JSON.');
        assert(json[r'code'] != null, 'Required key "SsoError[code]" has a null value in JSON.');
        assert(json.containsKey(r'message'), 'Required key "SsoError[message]" is missing from JSON.');
        assert(json[r'message'] != null, 'Required key "SsoError[message]" has a null value in JSON.');
        return true;
      }());

      return SsoError(
        code: mapValueOfType<String>(json, r'code')!,
        message: mapValueOfType<String>(json, r'message')!,
        email: mapValueOfType<String>(json, r'email'),
      );
    }
    return null;
  }

  static List<SsoError> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SsoError>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SsoError.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SsoError> mapFromJson(dynamic json) {
    final map = <String, SsoError>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SsoError.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SsoError-objects as value to a dart map
  static Map<String, List<SsoError>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SsoError>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SsoError.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'code',
    'message',
  };
}

