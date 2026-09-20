//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class RevisionList {
  /// Returns a new [RevisionList] instance.
  RevisionList({
    this.items = const [],
    this.nextCursor,
  });

  List<Revision> items;

  /// Absent at the end.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? nextCursor;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RevisionList &&
    _deepEquality.equals(other.items, items) &&
    other.nextCursor == nextCursor;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (items.hashCode) +
    (nextCursor == null ? 0 : nextCursor!.hashCode);

  @override
  String toString() => 'RevisionList[items=$items, nextCursor=$nextCursor]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'items'] = this.items;
    if (this.nextCursor != null) {
      json[r'next_cursor'] = this.nextCursor;
    } else {
      json[r'next_cursor'] = null;
    }
    return json;
  }

  /// Returns a new [RevisionList] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RevisionList? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'items'), 'Required key "RevisionList[items]" is missing from JSON.');
        assert(json[r'items'] != null, 'Required key "RevisionList[items]" has a null value in JSON.');
        return true;
      }());

      return RevisionList(
        items: Revision.listFromJson(json[r'items']),
        nextCursor: mapValueOfType<String>(json, r'next_cursor'),
      );
    }
    return null;
  }

  static List<RevisionList> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RevisionList>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RevisionList.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RevisionList> mapFromJson(dynamic json) {
    final map = <String, RevisionList>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RevisionList.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RevisionList-objects as value to a dart map
  static Map<String, List<RevisionList>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RevisionList>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RevisionList.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'items',
  };
}

