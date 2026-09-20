//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class DeferredItem {
  /// Returns a new [DeferredItem] instance.
  DeferredItem({
    required this.memoId,
    required this.capturedAt,
    this.durationMs,
    this.excerpt,
    this.reason,
    required this.heldBy,
    required this.heldAt,
    required this.ageSeconds,
  });

  String memoId;

  DateTime capturedAt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? durationMs;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? excerpt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? reason;

  String heldBy;

  DateTime heldAt;

  /// Computed SERVER-SIDE. The question is \"how long has this been waiting\", and a client computing it from two clocks would get a different answer from the one the backlog report gives. 
  int ageSeconds;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DeferredItem &&
    other.memoId == memoId &&
    other.capturedAt == capturedAt &&
    other.durationMs == durationMs &&
    other.excerpt == excerpt &&
    other.reason == reason &&
    other.heldBy == heldBy &&
    other.heldAt == heldAt &&
    other.ageSeconds == ageSeconds;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode) +
    (capturedAt.hashCode) +
    (durationMs == null ? 0 : durationMs!.hashCode) +
    (excerpt == null ? 0 : excerpt!.hashCode) +
    (reason == null ? 0 : reason!.hashCode) +
    (heldBy.hashCode) +
    (heldAt.hashCode) +
    (ageSeconds.hashCode);

  @override
  String toString() => 'DeferredItem[memoId=$memoId, capturedAt=$capturedAt, durationMs=$durationMs, excerpt=$excerpt, reason=$reason, heldBy=$heldBy, heldAt=$heldAt, ageSeconds=$ageSeconds]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
      json[r'captured_at'] = this.capturedAt.toUtc().toIso8601String();
    if (this.durationMs != null) {
      json[r'duration_ms'] = this.durationMs;
    } else {
      json[r'duration_ms'] = null;
    }
    if (this.excerpt != null) {
      json[r'excerpt'] = this.excerpt;
    } else {
      json[r'excerpt'] = null;
    }
    if (this.reason != null) {
      json[r'reason'] = this.reason;
    } else {
      json[r'reason'] = null;
    }
      json[r'held_by'] = this.heldBy;
      json[r'held_at'] = this.heldAt.toUtc().toIso8601String();
      json[r'age_seconds'] = this.ageSeconds;
    return json;
  }

  /// Returns a new [DeferredItem] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DeferredItem? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "DeferredItem[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "DeferredItem[memo_id]" has a null value in JSON.');
        assert(json.containsKey(r'captured_at'), 'Required key "DeferredItem[captured_at]" is missing from JSON.');
        assert(json[r'captured_at'] != null, 'Required key "DeferredItem[captured_at]" has a null value in JSON.');
        assert(json.containsKey(r'held_by'), 'Required key "DeferredItem[held_by]" is missing from JSON.');
        assert(json[r'held_by'] != null, 'Required key "DeferredItem[held_by]" has a null value in JSON.');
        assert(json.containsKey(r'held_at'), 'Required key "DeferredItem[held_at]" is missing from JSON.');
        assert(json[r'held_at'] != null, 'Required key "DeferredItem[held_at]" has a null value in JSON.');
        assert(json.containsKey(r'age_seconds'), 'Required key "DeferredItem[age_seconds]" is missing from JSON.');
        assert(json[r'age_seconds'] != null, 'Required key "DeferredItem[age_seconds]" has a null value in JSON.');
        return true;
      }());

      return DeferredItem(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        capturedAt: mapDateTime(json, r'captured_at', r'')!,
        durationMs: mapValueOfType<int>(json, r'duration_ms'),
        excerpt: mapValueOfType<String>(json, r'excerpt'),
        reason: mapValueOfType<String>(json, r'reason'),
        heldBy: mapValueOfType<String>(json, r'held_by')!,
        heldAt: mapDateTime(json, r'held_at', r'')!,
        ageSeconds: mapValueOfType<int>(json, r'age_seconds')!,
      );
    }
    return null;
  }

  static List<DeferredItem> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DeferredItem>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DeferredItem.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DeferredItem> mapFromJson(dynamic json) {
    final map = <String, DeferredItem>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DeferredItem.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DeferredItem-objects as value to a dart map
  static Map<String, List<DeferredItem>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DeferredItem>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DeferredItem.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
    'captured_at',
    'held_by',
    'held_at',
    'age_seconds',
  };
}

