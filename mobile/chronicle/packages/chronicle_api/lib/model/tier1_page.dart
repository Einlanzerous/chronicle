//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Tier1Page {
  /// Returns a new [Tier1Page] instance.
  Tier1Page({
    required this.path,
    required this.title,
    required this.body,
    required this.html,
    required this.generated,
  });

  /// The corpus's own address for the page, without the `.md`.
  String path;

  /// From the generator's front matter, else the first heading, else the last path segment.
  String title;

  /// The page's markdown, front matter removed.
  String body;

  /// The body rendered, safe to embed.
  String html;

  Generated generated;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Tier1Page &&
    other.path == path &&
    other.title == title &&
    other.body == body &&
    other.html == html &&
    other.generated == generated;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (path.hashCode) +
    (title.hashCode) +
    (body.hashCode) +
    (html.hashCode) +
    (generated.hashCode);

  @override
  String toString() => 'Tier1Page[path=$path, title=$title, body=$body, html=$html, generated=$generated]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'path'] = this.path;
      json[r'title'] = this.title;
      json[r'body'] = this.body;
      json[r'html'] = this.html;
      json[r'generated'] = this.generated;
    return json;
  }

  /// Returns a new [Tier1Page] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Tier1Page? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'path'), 'Required key "Tier1Page[path]" is missing from JSON.');
        assert(json[r'path'] != null, 'Required key "Tier1Page[path]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "Tier1Page[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "Tier1Page[title]" has a null value in JSON.');
        assert(json.containsKey(r'body'), 'Required key "Tier1Page[body]" is missing from JSON.');
        assert(json[r'body'] != null, 'Required key "Tier1Page[body]" has a null value in JSON.');
        assert(json.containsKey(r'html'), 'Required key "Tier1Page[html]" is missing from JSON.');
        assert(json[r'html'] != null, 'Required key "Tier1Page[html]" has a null value in JSON.');
        assert(json.containsKey(r'generated'), 'Required key "Tier1Page[generated]" is missing from JSON.');
        assert(json[r'generated'] != null, 'Required key "Tier1Page[generated]" has a null value in JSON.');
        return true;
      }());

      return Tier1Page(
        path: mapValueOfType<String>(json, r'path')!,
        title: mapValueOfType<String>(json, r'title')!,
        body: mapValueOfType<String>(json, r'body')!,
        html: mapValueOfType<String>(json, r'html')!,
        generated: Generated.fromJson(json[r'generated'])!,
      );
    }
    return null;
  }

  static List<Tier1Page> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Tier1Page>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Tier1Page.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Tier1Page> mapFromJson(dynamic json) {
    final map = <String, Tier1Page>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Tier1Page.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Tier1Page-objects as value to a dart map
  static Map<String, List<Tier1Page>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Tier1Page>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Tier1Page.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'path',
    'title',
    'body',
    'html',
    'generated',
  };
}

