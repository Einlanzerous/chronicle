//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Tier1PageSummary {
  /// Returns a new [Tier1PageSummary] instance.
  Tier1PageSummary({
    required this.path,
    required this.title,
  });

  /// The corpus's own address for the page, without the `.md`.
  String path;

  /// From the generator's front matter, else the first heading, else the last path segment.
  String title;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Tier1PageSummary &&
    other.path == path &&
    other.title == title;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (path.hashCode) +
    (title.hashCode);

  @override
  String toString() => 'Tier1PageSummary[path=$path, title=$title]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'path'] = this.path;
      json[r'title'] = this.title;
    return json;
  }

  /// Returns a new [Tier1PageSummary] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Tier1PageSummary? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'path'), 'Required key "Tier1PageSummary[path]" is missing from JSON.');
        assert(json[r'path'] != null, 'Required key "Tier1PageSummary[path]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "Tier1PageSummary[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "Tier1PageSummary[title]" has a null value in JSON.');
        return true;
      }());

      return Tier1PageSummary(
        path: mapValueOfType<String>(json, r'path')!,
        title: mapValueOfType<String>(json, r'title')!,
      );
    }
    return null;
  }

  static List<Tier1PageSummary> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Tier1PageSummary>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Tier1PageSummary.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Tier1PageSummary> mapFromJson(dynamic json) {
    final map = <String, Tier1PageSummary>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Tier1PageSummary.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Tier1PageSummary-objects as value to a dart map
  static Map<String, List<Tier1PageSummary>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Tier1PageSummary>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Tier1PageSummary.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'path',
    'title',
  };
}

