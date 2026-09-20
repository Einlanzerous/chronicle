//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Page {
  /// Returns a new [Page] instance.
  Page({
    required this.id,
    required this.path,
    required this.slug,
    this.parentId,
    required this.createdAt,
    required this.updatedAt,
  });

  String id;

  /// Derived from ancestry at read time, never stored — so it cannot be stale.
  String path;

  String slug;

  /// Absent on a root page.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? parentId;

  DateTime createdAt;

  DateTime updatedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Page &&
    other.id == id &&
    other.path == path &&
    other.slug == slug &&
    other.parentId == parentId &&
    other.createdAt == createdAt &&
    other.updatedAt == updatedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (path.hashCode) +
    (slug.hashCode) +
    (parentId == null ? 0 : parentId!.hashCode) +
    (createdAt.hashCode) +
    (updatedAt.hashCode);

  @override
  String toString() => 'Page[id=$id, path=$path, slug=$slug, parentId=$parentId, createdAt=$createdAt, updatedAt=$updatedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'path'] = this.path;
      json[r'slug'] = this.slug;
    if (this.parentId != null) {
      json[r'parent_id'] = this.parentId;
    } else {
      json[r'parent_id'] = null;
    }
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
      json[r'updated_at'] = this.updatedAt.toUtc().toIso8601String();
    return json;
  }

  /// Returns a new [Page] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Page? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "Page[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "Page[id]" has a null value in JSON.');
        assert(json.containsKey(r'path'), 'Required key "Page[path]" is missing from JSON.');
        assert(json[r'path'] != null, 'Required key "Page[path]" has a null value in JSON.');
        assert(json.containsKey(r'slug'), 'Required key "Page[slug]" is missing from JSON.');
        assert(json[r'slug'] != null, 'Required key "Page[slug]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "Page[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "Page[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'updated_at'), 'Required key "Page[updated_at]" is missing from JSON.');
        assert(json[r'updated_at'] != null, 'Required key "Page[updated_at]" has a null value in JSON.');
        return true;
      }());

      return Page(
        id: mapValueOfType<String>(json, r'id')!,
        path: mapValueOfType<String>(json, r'path')!,
        slug: mapValueOfType<String>(json, r'slug')!,
        parentId: mapValueOfType<String>(json, r'parent_id'),
        createdAt: mapDateTime(json, r'created_at', r'')!,
        updatedAt: mapDateTime(json, r'updated_at', r'')!,
      );
    }
    return null;
  }

  static List<Page> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Page>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Page.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Page> mapFromJson(dynamic json) {
    final map = <String, Page>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Page.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Page-objects as value to a dart map
  static Map<String, List<Page>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Page>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Page.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'path',
    'slug',
    'created_at',
    'updated_at',
  };
}

