//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class DeviceSession {
  /// Returns a new [DeviceSession] instance.
  DeviceSession({
    required this.id,
    required this.deviceLabel,
    required this.createdAt,
    required this.lastSeenAt,
    required this.current,
  });

  String id;

  String deviceLabel;

  DateTime createdAt;

  DateTime? lastSeenAt;

  /// Whether this is the session making the request. What lets a person revoke every device but the one in their hand. 
  bool current;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DeviceSession &&
    other.id == id &&
    other.deviceLabel == deviceLabel &&
    other.createdAt == createdAt &&
    other.lastSeenAt == lastSeenAt &&
    other.current == current;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (deviceLabel.hashCode) +
    (createdAt.hashCode) +
    (lastSeenAt == null ? 0 : lastSeenAt!.hashCode) +
    (current.hashCode);

  @override
  String toString() => 'DeviceSession[id=$id, deviceLabel=$deviceLabel, createdAt=$createdAt, lastSeenAt=$lastSeenAt, current=$current]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'device_label'] = this.deviceLabel;
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
    if (this.lastSeenAt != null) {
      json[r'last_seen_at'] = this.lastSeenAt!.toUtc().toIso8601String();
    } else {
      json[r'last_seen_at'] = null;
    }
      json[r'current'] = this.current;
    return json;
  }

  /// Returns a new [DeviceSession] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DeviceSession? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "DeviceSession[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "DeviceSession[id]" has a null value in JSON.');
        assert(json.containsKey(r'device_label'), 'Required key "DeviceSession[device_label]" is missing from JSON.');
        assert(json[r'device_label'] != null, 'Required key "DeviceSession[device_label]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "DeviceSession[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "DeviceSession[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'last_seen_at'), 'Required key "DeviceSession[last_seen_at]" is missing from JSON.');
        assert(json.containsKey(r'current'), 'Required key "DeviceSession[current]" is missing from JSON.');
        assert(json[r'current'] != null, 'Required key "DeviceSession[current]" has a null value in JSON.');
        return true;
      }());

      return DeviceSession(
        id: mapValueOfType<String>(json, r'id')!,
        deviceLabel: mapValueOfType<String>(json, r'device_label')!,
        createdAt: mapDateTime(json, r'created_at', r'')!,
        lastSeenAt: mapDateTime(json, r'last_seen_at', r''),
        current: mapValueOfType<bool>(json, r'current')!,
      );
    }
    return null;
  }

  static List<DeviceSession> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DeviceSession>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DeviceSession.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DeviceSession> mapFromJson(dynamic json) {
    final map = <String, DeviceSession>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DeviceSession.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DeviceSession-objects as value to a dart map
  static Map<String, List<DeviceSession>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DeviceSession>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DeviceSession.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'device_label',
    'created_at',
    'last_seen_at',
    'current',
  };
}

