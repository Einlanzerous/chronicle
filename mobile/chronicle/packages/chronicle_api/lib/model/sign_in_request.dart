//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SignInRequest {
  /// Returns a new [SignInRequest] instance.
  SignInRequest({
    required this.token,
    this.deviceLabel,
  });

  /// A single-use invite token.
  String token;

  /// What this device is called in the session list its holder reads. Optional, and bounded — it reaches a TEXT column with no length of its own. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? deviceLabel;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SignInRequest &&
    other.token == token &&
    other.deviceLabel == deviceLabel;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (token.hashCode) +
    (deviceLabel == null ? 0 : deviceLabel!.hashCode);

  @override
  String toString() => 'SignInRequest[token=$token, deviceLabel=$deviceLabel]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'token'] = this.token;
    if (this.deviceLabel != null) {
      json[r'device_label'] = this.deviceLabel;
    } else {
      json[r'device_label'] = null;
    }
    return json;
  }

  /// Returns a new [SignInRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SignInRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'token'), 'Required key "SignInRequest[token]" is missing from JSON.');
        assert(json[r'token'] != null, 'Required key "SignInRequest[token]" has a null value in JSON.');
        return true;
      }());

      return SignInRequest(
        token: mapValueOfType<String>(json, r'token')!,
        deviceLabel: mapValueOfType<String>(json, r'device_label'),
      );
    }
    return null;
  }

  static List<SignInRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SignInRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SignInRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SignInRequest> mapFromJson(dynamic json) {
    final map = <String, SignInRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SignInRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SignInRequest-objects as value to a dart map
  static Map<String, List<SignInRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SignInRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SignInRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'token',
  };
}

