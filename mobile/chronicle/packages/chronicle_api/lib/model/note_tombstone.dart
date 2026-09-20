//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class NoteTombstone {
  /// Returns a new [NoteTombstone] instance.
  NoteTombstone({
    required this.ref,
    required this.deletedAt,
    required this.deletedBy,
  });

  String ref;

  DateTime deletedAt;

  String deletedBy;

  @override
  bool operator ==(Object other) => identical(this, other) || other is NoteTombstone &&
    other.ref == ref &&
    other.deletedAt == deletedAt &&
    other.deletedBy == deletedBy;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (ref.hashCode) +
    (deletedAt.hashCode) +
    (deletedBy.hashCode);

  @override
  String toString() => 'NoteTombstone[ref=$ref, deletedAt=$deletedAt, deletedBy=$deletedBy]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'ref'] = this.ref;
      json[r'deleted_at'] = this.deletedAt.toUtc().toIso8601String();
      json[r'deleted_by'] = this.deletedBy;
    return json;
  }

  /// Returns a new [NoteTombstone] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static NoteTombstone? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'ref'), 'Required key "NoteTombstone[ref]" is missing from JSON.');
        assert(json[r'ref'] != null, 'Required key "NoteTombstone[ref]" has a null value in JSON.');
        assert(json.containsKey(r'deleted_at'), 'Required key "NoteTombstone[deleted_at]" is missing from JSON.');
        assert(json[r'deleted_at'] != null, 'Required key "NoteTombstone[deleted_at]" has a null value in JSON.');
        assert(json.containsKey(r'deleted_by'), 'Required key "NoteTombstone[deleted_by]" is missing from JSON.');
        assert(json[r'deleted_by'] != null, 'Required key "NoteTombstone[deleted_by]" has a null value in JSON.');
        return true;
      }());

      return NoteTombstone(
        ref: mapValueOfType<String>(json, r'ref')!,
        deletedAt: mapDateTime(json, r'deleted_at', r'')!,
        deletedBy: mapValueOfType<String>(json, r'deleted_by')!,
      );
    }
    return null;
  }

  static List<NoteTombstone> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <NoteTombstone>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = NoteTombstone.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, NoteTombstone> mapFromJson(dynamic json) {
    final map = <String, NoteTombstone>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = NoteTombstone.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of NoteTombstone-objects as value to a dart map
  static Map<String, List<NoteTombstone>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<NoteTombstone>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = NoteTombstone.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'ref',
    'deleted_at',
    'deleted_by',
  };
}

