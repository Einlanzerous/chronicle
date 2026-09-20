//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class BacklinkList {
  /// Returns a new [BacklinkList] instance.
  BacklinkList({
    this.items = const [],
    this.nextCursor,
    required this.generated,
  });

  /// Oldest source note first.
  List<Backlink> items;

  /// Absent at the end.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? nextCursor;

  Generated generated;

  @override
  bool operator ==(Object other) => identical(this, other) || other is BacklinkList &&
    _deepEquality.equals(other.items, items) &&
    other.nextCursor == nextCursor &&
    other.generated == generated;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (items.hashCode) +
    (nextCursor == null ? 0 : nextCursor!.hashCode) +
    (generated.hashCode);

  @override
  String toString() => 'BacklinkList[items=$items, nextCursor=$nextCursor, generated=$generated]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'items'] = this.items;
    if (this.nextCursor != null) {
      json[r'next_cursor'] = this.nextCursor;
    } else {
      json[r'next_cursor'] = null;
    }
      json[r'generated'] = this.generated;
    return json;
  }

  /// Returns a new [BacklinkList] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static BacklinkList? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'items'), 'Required key "BacklinkList[items]" is missing from JSON.');
        assert(json[r'items'] != null, 'Required key "BacklinkList[items]" has a null value in JSON.');
        assert(json.containsKey(r'generated'), 'Required key "BacklinkList[generated]" is missing from JSON.');
        assert(json[r'generated'] != null, 'Required key "BacklinkList[generated]" has a null value in JSON.');
        return true;
      }());

      return BacklinkList(
        items: Backlink.listFromJson(json[r'items']),
        nextCursor: mapValueOfType<String>(json, r'next_cursor'),
        generated: Generated.fromJson(json[r'generated'])!,
      );
    }
    return null;
  }

  static List<BacklinkList> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <BacklinkList>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = BacklinkList.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, BacklinkList> mapFromJson(dynamic json) {
    final map = <String, BacklinkList>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = BacklinkList.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of BacklinkList-objects as value to a dart map
  static Map<String, List<BacklinkList>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<BacklinkList>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = BacklinkList.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'items',
    'generated',
  };
}

