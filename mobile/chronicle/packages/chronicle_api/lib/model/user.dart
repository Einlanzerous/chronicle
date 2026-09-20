//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class User {
  /// Returns a new [User] instance.
  User({
    required this.id,
    required this.email,
    required this.displayName,
    required this.kind,
    required this.isOwner,
  });

  String id;

  String email;

  String displayName;

  String kind;

  bool isOwner;

  @override
  bool operator ==(Object other) => identical(this, other) || other is User &&
    other.id == id &&
    other.email == email &&
    other.displayName == displayName &&
    other.kind == kind &&
    other.isOwner == isOwner;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (email.hashCode) +
    (displayName.hashCode) +
    (kind.hashCode) +
    (isOwner.hashCode);

  @override
  String toString() => 'User[id=$id, email=$email, displayName=$displayName, kind=$kind, isOwner=$isOwner]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'email'] = this.email;
      json[r'display_name'] = this.displayName;
      json[r'kind'] = this.kind;
      json[r'is_owner'] = this.isOwner;
    return json;
  }

  /// Returns a new [User] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static User? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "User[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "User[id]" has a null value in JSON.');
        assert(json.containsKey(r'email'), 'Required key "User[email]" is missing from JSON.');
        assert(json[r'email'] != null, 'Required key "User[email]" has a null value in JSON.');
        assert(json.containsKey(r'display_name'), 'Required key "User[display_name]" is missing from JSON.');
        assert(json[r'display_name'] != null, 'Required key "User[display_name]" has a null value in JSON.');
        assert(json.containsKey(r'kind'), 'Required key "User[kind]" is missing from JSON.');
        assert(json[r'kind'] != null, 'Required key "User[kind]" has a null value in JSON.');
        assert(json.containsKey(r'is_owner'), 'Required key "User[is_owner]" is missing from JSON.');
        assert(json[r'is_owner'] != null, 'Required key "User[is_owner]" has a null value in JSON.');
        return true;
      }());

      return User(
        id: mapValueOfType<String>(json, r'id')!,
        email: mapValueOfType<String>(json, r'email')!,
        displayName: mapValueOfType<String>(json, r'display_name')!,
        kind: mapValueOfType<String>(json, r'kind')!,
        isOwner: mapValueOfType<bool>(json, r'is_owner')!,
      );
    }
    return null;
  }

  static List<User> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <User>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = User.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, User> mapFromJson(dynamic json) {
    final map = <String, User>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = User.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of User-objects as value to a dart map
  static Map<String, List<User>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<User>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = User.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'email',
    'display_name',
    'kind',
    'is_owner',
  };
}

