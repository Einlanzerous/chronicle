//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Mismatch {
  /// Returns a new [Mismatch] instance.
  Mismatch({
    required this.ref,
    required this.onDiskBytes,
    required this.recordedBytes,
  });

  /// `<author-id>/<content-hash>`.
  String ref;

  int onDiskBytes;

  int recordedBytes;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Mismatch &&
    other.ref == ref &&
    other.onDiskBytes == onDiskBytes &&
    other.recordedBytes == recordedBytes;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (ref.hashCode) +
    (onDiskBytes.hashCode) +
    (recordedBytes.hashCode);

  @override
  String toString() => 'Mismatch[ref=$ref, onDiskBytes=$onDiskBytes, recordedBytes=$recordedBytes]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'ref'] = this.ref;
      json[r'on_disk_bytes'] = this.onDiskBytes;
      json[r'recorded_bytes'] = this.recordedBytes;
    return json;
  }

  /// Returns a new [Mismatch] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Mismatch? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'ref'), 'Required key "Mismatch[ref]" is missing from JSON.');
        assert(json[r'ref'] != null, 'Required key "Mismatch[ref]" has a null value in JSON.');
        assert(json.containsKey(r'on_disk_bytes'), 'Required key "Mismatch[on_disk_bytes]" is missing from JSON.');
        assert(json[r'on_disk_bytes'] != null, 'Required key "Mismatch[on_disk_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'recorded_bytes'), 'Required key "Mismatch[recorded_bytes]" is missing from JSON.');
        assert(json[r'recorded_bytes'] != null, 'Required key "Mismatch[recorded_bytes]" has a null value in JSON.');
        return true;
      }());

      return Mismatch(
        ref: mapValueOfType<String>(json, r'ref')!,
        onDiskBytes: mapValueOfType<int>(json, r'on_disk_bytes')!,
        recordedBytes: mapValueOfType<int>(json, r'recorded_bytes')!,
      );
    }
    return null;
  }

  static List<Mismatch> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Mismatch>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Mismatch.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Mismatch> mapFromJson(dynamic json) {
    final map = <String, Mismatch>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Mismatch.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Mismatch-objects as value to a dart map
  static Map<String, List<Mismatch>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Mismatch>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Mismatch.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'ref',
    'on_disk_bytes',
    'recorded_bytes',
  };
}

