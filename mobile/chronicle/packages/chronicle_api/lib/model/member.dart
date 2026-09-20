//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Member {
  /// Returns a new [Member] instance.
  Member({
    required this.id,
    required this.email,
    required this.displayName,
    required this.kind,
    required this.isOwner,
    required this.lastSeenAt,
    required this.inviteExpiresAt,
    required this.sessionCount,
  });

  String id;

  String email;

  String displayName;

  String kind;

  bool isOwner;

  DateTime? lastSeenAt;

  DateTime? inviteExpiresAt;

  int sessionCount;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Member &&
    other.id == id &&
    other.email == email &&
    other.displayName == displayName &&
    other.kind == kind &&
    other.isOwner == isOwner &&
    other.lastSeenAt == lastSeenAt &&
    other.inviteExpiresAt == inviteExpiresAt &&
    other.sessionCount == sessionCount;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (email.hashCode) +
    (displayName.hashCode) +
    (kind.hashCode) +
    (isOwner.hashCode) +
    (lastSeenAt == null ? 0 : lastSeenAt!.hashCode) +
    (inviteExpiresAt == null ? 0 : inviteExpiresAt!.hashCode) +
    (sessionCount.hashCode);

  @override
  String toString() => 'Member[id=$id, email=$email, displayName=$displayName, kind=$kind, isOwner=$isOwner, lastSeenAt=$lastSeenAt, inviteExpiresAt=$inviteExpiresAt, sessionCount=$sessionCount]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'email'] = this.email;
      json[r'display_name'] = this.displayName;
      json[r'kind'] = this.kind;
      json[r'is_owner'] = this.isOwner;
    if (this.lastSeenAt != null) {
      json[r'last_seen_at'] = this.lastSeenAt!.toUtc().toIso8601String();
    } else {
      json[r'last_seen_at'] = null;
    }
    if (this.inviteExpiresAt != null) {
      json[r'invite_expires_at'] = this.inviteExpiresAt!.toUtc().toIso8601String();
    } else {
      json[r'invite_expires_at'] = null;
    }
      json[r'session_count'] = this.sessionCount;
    return json;
  }

  /// Returns a new [Member] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Member? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "Member[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "Member[id]" has a null value in JSON.');
        assert(json.containsKey(r'email'), 'Required key "Member[email]" is missing from JSON.');
        assert(json[r'email'] != null, 'Required key "Member[email]" has a null value in JSON.');
        assert(json.containsKey(r'display_name'), 'Required key "Member[display_name]" is missing from JSON.');
        assert(json[r'display_name'] != null, 'Required key "Member[display_name]" has a null value in JSON.');
        assert(json.containsKey(r'kind'), 'Required key "Member[kind]" is missing from JSON.');
        assert(json[r'kind'] != null, 'Required key "Member[kind]" has a null value in JSON.');
        assert(json.containsKey(r'is_owner'), 'Required key "Member[is_owner]" is missing from JSON.');
        assert(json[r'is_owner'] != null, 'Required key "Member[is_owner]" has a null value in JSON.');
        assert(json.containsKey(r'last_seen_at'), 'Required key "Member[last_seen_at]" is missing from JSON.');
        assert(json.containsKey(r'invite_expires_at'), 'Required key "Member[invite_expires_at]" is missing from JSON.');
        assert(json.containsKey(r'session_count'), 'Required key "Member[session_count]" is missing from JSON.');
        assert(json[r'session_count'] != null, 'Required key "Member[session_count]" has a null value in JSON.');
        return true;
      }());

      return Member(
        id: mapValueOfType<String>(json, r'id')!,
        email: mapValueOfType<String>(json, r'email')!,
        displayName: mapValueOfType<String>(json, r'display_name')!,
        kind: mapValueOfType<String>(json, r'kind')!,
        isOwner: mapValueOfType<bool>(json, r'is_owner')!,
        lastSeenAt: mapDateTime(json, r'last_seen_at', r''),
        inviteExpiresAt: mapDateTime(json, r'invite_expires_at', r''),
        sessionCount: mapValueOfType<int>(json, r'session_count')!,
      );
    }
    return null;
  }

  static List<Member> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Member>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Member.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Member> mapFromJson(dynamic json) {
    final map = <String, Member>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Member.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Member-objects as value to a dart map
  static Map<String, List<Member>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Member>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Member.listFromJson(entry.value, growable: growable,);
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
    'last_seen_at',
    'invite_expires_at',
    'session_count',
  };
}

