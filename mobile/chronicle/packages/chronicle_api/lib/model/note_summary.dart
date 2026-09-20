//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class NoteSummary {
  /// Returns a new [NoteSummary] instance.
  NoteSummary({
    required this.ref,
    required this.title,
    required this.page,
    required this.createdAt,
    required this.updatedAt,
  });

  /// `CHR-0311`. Permanent: the number is minted once and never reused.
  String ref;

  String title;

  /// The path of the page it is filed on, current as of this read.
  String page;

  DateTime createdAt;

  DateTime updatedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is NoteSummary &&
    other.ref == ref &&
    other.title == title &&
    other.page == page &&
    other.createdAt == createdAt &&
    other.updatedAt == updatedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (ref.hashCode) +
    (title.hashCode) +
    (page.hashCode) +
    (createdAt.hashCode) +
    (updatedAt.hashCode);

  @override
  String toString() => 'NoteSummary[ref=$ref, title=$title, page=$page, createdAt=$createdAt, updatedAt=$updatedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'ref'] = this.ref;
      json[r'title'] = this.title;
      json[r'page'] = this.page;
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
      json[r'updated_at'] = this.updatedAt.toUtc().toIso8601String();
    return json;
  }

  /// Returns a new [NoteSummary] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static NoteSummary? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'ref'), 'Required key "NoteSummary[ref]" is missing from JSON.');
        assert(json[r'ref'] != null, 'Required key "NoteSummary[ref]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "NoteSummary[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "NoteSummary[title]" has a null value in JSON.');
        assert(json.containsKey(r'page'), 'Required key "NoteSummary[page]" is missing from JSON.');
        assert(json[r'page'] != null, 'Required key "NoteSummary[page]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "NoteSummary[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "NoteSummary[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'updated_at'), 'Required key "NoteSummary[updated_at]" is missing from JSON.');
        assert(json[r'updated_at'] != null, 'Required key "NoteSummary[updated_at]" has a null value in JSON.');
        return true;
      }());

      return NoteSummary(
        ref: mapValueOfType<String>(json, r'ref')!,
        title: mapValueOfType<String>(json, r'title')!,
        page: mapValueOfType<String>(json, r'page')!,
        createdAt: mapDateTime(json, r'created_at', r'')!,
        updatedAt: mapDateTime(json, r'updated_at', r'')!,
      );
    }
    return null;
  }

  static List<NoteSummary> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <NoteSummary>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = NoteSummary.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, NoteSummary> mapFromJson(dynamic json) {
    final map = <String, NoteSummary>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = NoteSummary.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of NoteSummary-objects as value to a dart map
  static Map<String, List<NoteSummary>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<NoteSummary>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = NoteSummary.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'ref',
    'title',
    'page',
    'created_at',
    'updated_at',
  };
}

