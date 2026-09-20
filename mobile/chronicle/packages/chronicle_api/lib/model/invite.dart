//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Invite {
  /// Returns a new [Invite] instance.
  Invite({
    required this.user,
    required this.inviteToken,
    this.signInUrl,
    required this.expiresIn,
  });

  User user;

  String inviteToken;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? signInUrl;

  /// How long the invite is good for, as a Go duration string.
  String expiresIn;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Invite &&
    other.user == user &&
    other.inviteToken == inviteToken &&
    other.signInUrl == signInUrl &&
    other.expiresIn == expiresIn;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (user.hashCode) +
    (inviteToken.hashCode) +
    (signInUrl == null ? 0 : signInUrl!.hashCode) +
    (expiresIn.hashCode);

  @override
  String toString() => 'Invite[user=$user, inviteToken=$inviteToken, signInUrl=$signInUrl, expiresIn=$expiresIn]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'user'] = this.user;
      json[r'invite_token'] = this.inviteToken;
    if (this.signInUrl != null) {
      json[r'sign_in_url'] = this.signInUrl;
    } else {
      json[r'sign_in_url'] = null;
    }
      json[r'expires_in'] = this.expiresIn;
    return json;
  }

  /// Returns a new [Invite] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Invite? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'user'), 'Required key "Invite[user]" is missing from JSON.');
        assert(json[r'user'] != null, 'Required key "Invite[user]" has a null value in JSON.');
        assert(json.containsKey(r'invite_token'), 'Required key "Invite[invite_token]" is missing from JSON.');
        assert(json[r'invite_token'] != null, 'Required key "Invite[invite_token]" has a null value in JSON.');
        assert(json.containsKey(r'expires_in'), 'Required key "Invite[expires_in]" is missing from JSON.');
        assert(json[r'expires_in'] != null, 'Required key "Invite[expires_in]" has a null value in JSON.');
        return true;
      }());

      return Invite(
        user: User.fromJson(json[r'user'])!,
        inviteToken: mapValueOfType<String>(json, r'invite_token')!,
        signInUrl: mapValueOfType<String>(json, r'sign_in_url'),
        expiresIn: mapValueOfType<String>(json, r'expires_in')!,
      );
    }
    return null;
  }

  static List<Invite> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Invite>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Invite.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Invite> mapFromJson(dynamic json) {
    final map = <String, Invite>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Invite.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Invite-objects as value to a dart map
  static Map<String, List<Invite>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Invite>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Invite.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'user',
    'invite_token',
    'expires_in',
  };
}

