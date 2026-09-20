//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Tier1PageList {
  /// Returns a new [Tier1PageList] instance.
  Tier1PageList({
    this.items = const [],
    required this.generated,
  });

  List<Tier1PageSummary> items;

  Generated generated;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Tier1PageList &&
    _deepEquality.equals(other.items, items) &&
    other.generated == generated;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (items.hashCode) +
    (generated.hashCode);

  @override
  String toString() => 'Tier1PageList[items=$items, generated=$generated]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'items'] = this.items;
      json[r'generated'] = this.generated;
    return json;
  }

  /// Returns a new [Tier1PageList] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Tier1PageList? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'items'), 'Required key "Tier1PageList[items]" is missing from JSON.');
        assert(json[r'items'] != null, 'Required key "Tier1PageList[items]" has a null value in JSON.');
        assert(json.containsKey(r'generated'), 'Required key "Tier1PageList[generated]" is missing from JSON.');
        assert(json[r'generated'] != null, 'Required key "Tier1PageList[generated]" has a null value in JSON.');
        return true;
      }());

      return Tier1PageList(
        items: Tier1PageSummary.listFromJson(json[r'items']),
        generated: Generated.fromJson(json[r'generated'])!,
      );
    }
    return null;
  }

  static List<Tier1PageList> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Tier1PageList>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Tier1PageList.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Tier1PageList> mapFromJson(dynamic json) {
    final map = <String, Tier1PageList>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Tier1PageList.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Tier1PageList-objects as value to a dart map
  static Map<String, List<Tier1PageList>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Tier1PageList>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Tier1PageList.listFromJson(entry.value, growable: growable,);
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

