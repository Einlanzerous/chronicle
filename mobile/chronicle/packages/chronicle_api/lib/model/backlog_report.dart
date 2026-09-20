//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class BacklogReport {
  /// Returns a new [BacklogReport] instance.
  BacklogReport({
    required this.total,
    required this.today,
    required this.thisWeek,
    required this.older,
    this.oldestCapturedAt,
  });

  int total;

  int today;

  int thisWeek;

  int older;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? oldestCapturedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is BacklogReport &&
    other.total == total &&
    other.today == today &&
    other.thisWeek == thisWeek &&
    other.older == older &&
    other.oldestCapturedAt == oldestCapturedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (total.hashCode) +
    (today.hashCode) +
    (thisWeek.hashCode) +
    (older.hashCode) +
    (oldestCapturedAt == null ? 0 : oldestCapturedAt!.hashCode);

  @override
  String toString() => 'BacklogReport[total=$total, today=$today, thisWeek=$thisWeek, older=$older, oldestCapturedAt=$oldestCapturedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'total'] = this.total;
      json[r'today'] = this.today;
      json[r'this_week'] = this.thisWeek;
      json[r'older'] = this.older;
    if (this.oldestCapturedAt != null) {
      json[r'oldest_captured_at'] = this.oldestCapturedAt!.toUtc().toIso8601String();
    } else {
      json[r'oldest_captured_at'] = null;
    }
    return json;
  }

  /// Returns a new [BacklogReport] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static BacklogReport? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'total'), 'Required key "BacklogReport[total]" is missing from JSON.');
        assert(json[r'total'] != null, 'Required key "BacklogReport[total]" has a null value in JSON.');
        assert(json.containsKey(r'today'), 'Required key "BacklogReport[today]" is missing from JSON.');
        assert(json[r'today'] != null, 'Required key "BacklogReport[today]" has a null value in JSON.');
        assert(json.containsKey(r'this_week'), 'Required key "BacklogReport[this_week]" is missing from JSON.');
        assert(json[r'this_week'] != null, 'Required key "BacklogReport[this_week]" has a null value in JSON.');
        assert(json.containsKey(r'older'), 'Required key "BacklogReport[older]" is missing from JSON.');
        assert(json[r'older'] != null, 'Required key "BacklogReport[older]" has a null value in JSON.');
        return true;
      }());

      return BacklogReport(
        total: mapValueOfType<int>(json, r'total')!,
        today: mapValueOfType<int>(json, r'today')!,
        thisWeek: mapValueOfType<int>(json, r'this_week')!,
        older: mapValueOfType<int>(json, r'older')!,
        oldestCapturedAt: mapDateTime(json, r'oldest_captured_at', r''),
      );
    }
    return null;
  }

  static List<BacklogReport> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <BacklogReport>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = BacklogReport.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, BacklogReport> mapFromJson(dynamic json) {
    final map = <String, BacklogReport>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = BacklogReport.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of BacklogReport-objects as value to a dart map
  static Map<String, List<BacklogReport>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<BacklogReport>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = BacklogReport.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'total',
    'today',
    'this_week',
    'older',
  };
}

