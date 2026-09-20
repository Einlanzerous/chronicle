//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class PageTree {
  /// Returns a new [PageTree] instance.
  PageTree({
    this.paths = const [],
  });

  /// Every page's current path, sorted. The tree is a property of the strings.
  List<String> paths;

  @override
  bool operator ==(Object other) => identical(this, other) || other is PageTree &&
    _deepEquality.equals(other.paths, paths);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (paths.hashCode);

  @override
  String toString() => 'PageTree[paths=$paths]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'paths'] = this.paths;
    return json;
  }

  /// Returns a new [PageTree] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static PageTree? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'paths'), 'Required key "PageTree[paths]" is missing from JSON.');
        assert(json[r'paths'] != null, 'Required key "PageTree[paths]" has a null value in JSON.');
        return true;
      }());

      return PageTree(
        paths: json[r'paths'] is Iterable
            ? (json[r'paths'] as Iterable).cast<String>().toList(growable: false)
            : const [],
      );
    }
    return null;
  }

  static List<PageTree> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <PageTree>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = PageTree.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, PageTree> mapFromJson(dynamic json) {
    final map = <String, PageTree>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = PageTree.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of PageTree-objects as value to a dart map
  static Map<String, List<PageTree>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<PageTree>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = PageTree.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'paths',
  };
}

