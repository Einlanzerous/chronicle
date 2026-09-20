//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class MarkReadRequest {
  /// Returns a new [MarkReadRequest] instance.
  MarkReadRequest({
    required this.throughSeq,
  });

  /// The highest `seq` the client has read. Clamped to the thread's last turn; never moves the marker backwards.
  ///
  /// Minimum value: 0
  int throughSeq;

  @override
  bool operator ==(Object other) => identical(this, other) || other is MarkReadRequest &&
    other.throughSeq == throughSeq;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (throughSeq.hashCode);

  @override
  String toString() => 'MarkReadRequest[throughSeq=$throughSeq]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'through_seq'] = this.throughSeq;
    return json;
  }

  /// Returns a new [MarkReadRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static MarkReadRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'through_seq'), 'Required key "MarkReadRequest[through_seq]" is missing from JSON.');
        assert(json[r'through_seq'] != null, 'Required key "MarkReadRequest[through_seq]" has a null value in JSON.');
        return true;
      }());

      return MarkReadRequest(
        throughSeq: mapValueOfType<int>(json, r'through_seq')!,
      );
    }
    return null;
  }

  static List<MarkReadRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <MarkReadRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = MarkReadRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, MarkReadRequest> mapFromJson(dynamic json) {
    final map = <String, MarkReadRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = MarkReadRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of MarkReadRequest-objects as value to a dart map
  static Map<String, List<MarkReadRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<MarkReadRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = MarkReadRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'through_seq',
  };
}

