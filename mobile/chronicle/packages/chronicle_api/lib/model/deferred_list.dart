//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class DeferredList {
  /// Returns a new [DeferredList] instance.
  DeferredList({
    this.items = const [],
    required this.limit,
  });

  List<DeferredItem> items;

  int limit;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DeferredList &&
    _deepEquality.equals(other.items, items) &&
    other.limit == limit;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (items.hashCode) +
    (limit.hashCode);

  @override
  String toString() => 'DeferredList[items=$items, limit=$limit]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'items'] = this.items;
      json[r'limit'] = this.limit;
    return json;
  }

  /// Returns a new [DeferredList] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DeferredList? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'items'), 'Required key "DeferredList[items]" is missing from JSON.');
        assert(json[r'items'] != null, 'Required key "DeferredList[items]" has a null value in JSON.');
        assert(json.containsKey(r'limit'), 'Required key "DeferredList[limit]" is missing from JSON.');
        assert(json[r'limit'] != null, 'Required key "DeferredList[limit]" has a null value in JSON.');
        return true;
      }());

      return DeferredList(
        items: DeferredItem.listFromJson(json[r'items']),
        limit: mapValueOfType<int>(json, r'limit')!,
      );
    }
    return null;
  }

  static List<DeferredList> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DeferredList>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DeferredList.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DeferredList> mapFromJson(dynamic json) {
    final map = <String, DeferredList>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DeferredList.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DeferredList-objects as value to a dart map
  static Map<String, List<DeferredList>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DeferredList>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DeferredList.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'items',
    'limit',
  };
}

