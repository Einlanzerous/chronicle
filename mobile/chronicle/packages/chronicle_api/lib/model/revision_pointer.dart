//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class RevisionPointer {
  /// Returns a new [RevisionPointer] instance.
  RevisionPointer({
    required this.id,
    required this.seq,
  });

  String id;

  int seq;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RevisionPointer &&
    other.id == id &&
    other.seq == seq;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (seq.hashCode);

  @override
  String toString() => 'RevisionPointer[id=$id, seq=$seq]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'seq'] = this.seq;
    return json;
  }

  /// Returns a new [RevisionPointer] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RevisionPointer? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "RevisionPointer[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "RevisionPointer[id]" has a null value in JSON.');
        assert(json.containsKey(r'seq'), 'Required key "RevisionPointer[seq]" is missing from JSON.');
        assert(json[r'seq'] != null, 'Required key "RevisionPointer[seq]" has a null value in JSON.');
        return true;
      }());

      return RevisionPointer(
        id: mapValueOfType<String>(json, r'id')!,
        seq: mapValueOfType<int>(json, r'seq')!,
      );
    }
    return null;
  }

  static List<RevisionPointer> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RevisionPointer>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RevisionPointer.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RevisionPointer> mapFromJson(dynamic json) {
    final map = <String, RevisionPointer>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RevisionPointer.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RevisionPointer-objects as value to a dart map
  static Map<String, List<RevisionPointer>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RevisionPointer>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RevisionPointer.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'seq',
  };
}

