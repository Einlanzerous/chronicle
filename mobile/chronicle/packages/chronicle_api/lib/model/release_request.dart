//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ReleaseRequest {
  /// Returns a new [ReleaseRequest] instance.
  ReleaseRequest({
    required this.memoId,
  });

  String memoId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ReleaseRequest &&
    other.memoId == memoId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode);

  @override
  String toString() => 'ReleaseRequest[memoId=$memoId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
    return json;
  }

  /// Returns a new [ReleaseRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ReleaseRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "ReleaseRequest[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "ReleaseRequest[memo_id]" has a null value in JSON.');
        return true;
      }());

      return ReleaseRequest(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
      );
    }
    return null;
  }

  static List<ReleaseRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ReleaseRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ReleaseRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ReleaseRequest> mapFromJson(dynamic json) {
    final map = <String, ReleaseRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ReleaseRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ReleaseRequest-objects as value to a dart map
  static Map<String, List<ReleaseRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ReleaseRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ReleaseRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
  };
}

