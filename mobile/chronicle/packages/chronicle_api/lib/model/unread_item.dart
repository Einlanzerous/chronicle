//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class UnreadItem {
  /// Returns a new [UnreadItem] instance.
  UnreadItem({
    required this.ref,
    required this.title,
    required this.unread,
  });

  String ref;

  String title;

  /// Minimum value: 1
  int unread;

  @override
  bool operator ==(Object other) => identical(this, other) || other is UnreadItem &&
    other.ref == ref &&
    other.title == title &&
    other.unread == unread;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (ref.hashCode) +
    (title.hashCode) +
    (unread.hashCode);

  @override
  String toString() => 'UnreadItem[ref=$ref, title=$title, unread=$unread]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'ref'] = this.ref;
      json[r'title'] = this.title;
      json[r'unread'] = this.unread;
    return json;
  }

  /// Returns a new [UnreadItem] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static UnreadItem? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'ref'), 'Required key "UnreadItem[ref]" is missing from JSON.');
        assert(json[r'ref'] != null, 'Required key "UnreadItem[ref]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "UnreadItem[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "UnreadItem[title]" has a null value in JSON.');
        assert(json.containsKey(r'unread'), 'Required key "UnreadItem[unread]" is missing from JSON.');
        assert(json[r'unread'] != null, 'Required key "UnreadItem[unread]" has a null value in JSON.');
        return true;
      }());

      return UnreadItem(
        ref: mapValueOfType<String>(json, r'ref')!,
        title: mapValueOfType<String>(json, r'title')!,
        unread: mapValueOfType<int>(json, r'unread')!,
      );
    }
    return null;
  }

  static List<UnreadItem> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <UnreadItem>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = UnreadItem.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, UnreadItem> mapFromJson(dynamic json) {
    final map = <String, UnreadItem>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = UnreadItem.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of UnreadItem-objects as value to a dart map
  static Map<String, List<UnreadItem>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<UnreadItem>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = UnreadItem.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'ref',
    'title',
    'unread',
  };
}

