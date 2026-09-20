//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class HeldMemo {
  /// Returns a new [HeldMemo] instance.
  HeldMemo({
    required this.id,
    required this.authorId,
    required this.capturedAt,
    required this.reason,
    required this.retry,
  });

  String id;

  String authorId;

  DateTime capturedAt;

  String reason;

  /// The exact command that releases this memo.
  String retry;

  @override
  bool operator ==(Object other) => identical(this, other) || other is HeldMemo &&
    other.id == id &&
    other.authorId == authorId &&
    other.capturedAt == capturedAt &&
    other.reason == reason &&
    other.retry == retry;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (authorId.hashCode) +
    (capturedAt.hashCode) +
    (reason.hashCode) +
    (retry.hashCode);

  @override
  String toString() => 'HeldMemo[id=$id, authorId=$authorId, capturedAt=$capturedAt, reason=$reason, retry=$retry]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'author_id'] = this.authorId;
      json[r'captured_at'] = this.capturedAt.toUtc().toIso8601String();
      json[r'reason'] = this.reason;
      json[r'retry'] = this.retry;
    return json;
  }

  /// Returns a new [HeldMemo] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static HeldMemo? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "HeldMemo[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "HeldMemo[id]" has a null value in JSON.');
        assert(json.containsKey(r'author_id'), 'Required key "HeldMemo[author_id]" is missing from JSON.');
        assert(json[r'author_id'] != null, 'Required key "HeldMemo[author_id]" has a null value in JSON.');
        assert(json.containsKey(r'captured_at'), 'Required key "HeldMemo[captured_at]" is missing from JSON.');
        assert(json[r'captured_at'] != null, 'Required key "HeldMemo[captured_at]" has a null value in JSON.');
        assert(json.containsKey(r'reason'), 'Required key "HeldMemo[reason]" is missing from JSON.');
        assert(json[r'reason'] != null, 'Required key "HeldMemo[reason]" has a null value in JSON.');
        assert(json.containsKey(r'retry'), 'Required key "HeldMemo[retry]" is missing from JSON.');
        assert(json[r'retry'] != null, 'Required key "HeldMemo[retry]" has a null value in JSON.');
        return true;
      }());

      return HeldMemo(
        id: mapValueOfType<String>(json, r'id')!,
        authorId: mapValueOfType<String>(json, r'author_id')!,
        capturedAt: mapDateTime(json, r'captured_at', r'')!,
        reason: mapValueOfType<String>(json, r'reason')!,
        retry: mapValueOfType<String>(json, r'retry')!,
      );
    }
    return null;
  }

  static List<HeldMemo> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <HeldMemo>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = HeldMemo.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, HeldMemo> mapFromJson(dynamic json) {
    final map = <String, HeldMemo>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = HeldMemo.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of HeldMemo-objects as value to a dart map
  static Map<String, List<HeldMemo>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<HeldMemo>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = HeldMemo.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'author_id',
    'captured_at',
    'reason',
    'retry',
  };
}

