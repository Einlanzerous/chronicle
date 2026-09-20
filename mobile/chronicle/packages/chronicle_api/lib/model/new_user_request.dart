//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class NewUserRequest {
  /// Returns a new [NewUserRequest] instance.
  NewUserRequest({
    required this.email,
    this.displayName,
    this.kind,
  });

  /// Also the Access identity this account is matched to on SSO.
  String email;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? displayName;

  /// `person` or `agent`. An agent account can hold a token but is never an owner and never confirms authored text — `note_revisions_guard` refuses any revision whose `confirmed_by` names one. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? kind;

  @override
  bool operator ==(Object other) => identical(this, other) || other is NewUserRequest &&
    other.email == email &&
    other.displayName == displayName &&
    other.kind == kind;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (email.hashCode) +
    (displayName == null ? 0 : displayName!.hashCode) +
    (kind == null ? 0 : kind!.hashCode);

  @override
  String toString() => 'NewUserRequest[email=$email, displayName=$displayName, kind=$kind]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'email'] = this.email;
    if (this.displayName != null) {
      json[r'display_name'] = this.displayName;
    } else {
      json[r'display_name'] = null;
    }
    if (this.kind != null) {
      json[r'kind'] = this.kind;
    } else {
      json[r'kind'] = null;
    }
    return json;
  }

  /// Returns a new [NewUserRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static NewUserRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'email'), 'Required key "NewUserRequest[email]" is missing from JSON.');
        assert(json[r'email'] != null, 'Required key "NewUserRequest[email]" has a null value in JSON.');
        return true;
      }());

      return NewUserRequest(
        email: mapValueOfType<String>(json, r'email')!,
        displayName: mapValueOfType<String>(json, r'display_name'),
        kind: mapValueOfType<String>(json, r'kind'),
      );
    }
    return null;
  }

  static List<NewUserRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <NewUserRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = NewUserRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, NewUserRequest> mapFromJson(dynamic json) {
    final map = <String, NewUserRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = NewUserRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of NewUserRequest-objects as value to a dart map
  static Map<String, List<NewUserRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<NewUserRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = NewUserRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'email',
  };
}

