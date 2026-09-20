//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Discussion {
  /// Returns a new [Discussion] instance.
  Discussion({
    required this.ref,
    required this.title,
    this.page,
    required this.createdAt,
    this.resolved,
  });

  /// `DSC-0007`. Permanent.
  String ref;

  String title;

  /// The path of the page it is filed against, current as of this read. Absent for an unfiled thread — a thread is not addressed until it resolves.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? page;

  DateTime createdAt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  ResolvedState? resolved;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Discussion &&
    other.ref == ref &&
    other.title == title &&
    other.page == page &&
    other.createdAt == createdAt &&
    other.resolved == resolved;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (ref.hashCode) +
    (title.hashCode) +
    (page == null ? 0 : page!.hashCode) +
    (createdAt.hashCode) +
    (resolved == null ? 0 : resolved!.hashCode);

  @override
  String toString() => 'Discussion[ref=$ref, title=$title, page=$page, createdAt=$createdAt, resolved=$resolved]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'ref'] = this.ref;
      json[r'title'] = this.title;
    if (this.page != null) {
      json[r'page'] = this.page;
    } else {
      json[r'page'] = null;
    }
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
    if (this.resolved != null) {
      json[r'resolved'] = this.resolved;
    } else {
      json[r'resolved'] = null;
    }
    return json;
  }

  /// Returns a new [Discussion] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Discussion? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'ref'), 'Required key "Discussion[ref]" is missing from JSON.');
        assert(json[r'ref'] != null, 'Required key "Discussion[ref]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "Discussion[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "Discussion[title]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "Discussion[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "Discussion[created_at]" has a null value in JSON.');
        return true;
      }());

      return Discussion(
        ref: mapValueOfType<String>(json, r'ref')!,
        title: mapValueOfType<String>(json, r'title')!,
        page: mapValueOfType<String>(json, r'page'),
        createdAt: mapDateTime(json, r'created_at', r'')!,
        resolved: ResolvedState.fromJson(json[r'resolved']),
      );
    }
    return null;
  }

  static List<Discussion> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Discussion>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Discussion.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Discussion> mapFromJson(dynamic json) {
    final map = <String, Discussion>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Discussion.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Discussion-objects as value to a dart map
  static Map<String, List<Discussion>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Discussion>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Discussion.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'ref',
    'title',
    'created_at',
  };
}

