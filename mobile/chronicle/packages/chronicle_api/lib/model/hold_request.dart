//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class HoldRequest {
  /// Returns a new [HoldRequest] instance.
  HoldRequest({
    required this.memoId,
    this.reason,
  });

  String memoId;

  /// Optional. Most deferrals are \"not now\"; the ones that are not are the ones still legible in three weeks. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? reason;

  @override
  bool operator ==(Object other) => identical(this, other) || other is HoldRequest &&
    other.memoId == memoId &&
    other.reason == reason;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode) +
    (reason == null ? 0 : reason!.hashCode);

  @override
  String toString() => 'HoldRequest[memoId=$memoId, reason=$reason]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
    if (this.reason != null) {
      json[r'reason'] = this.reason;
    } else {
      json[r'reason'] = null;
    }
    return json;
  }

  /// Returns a new [HoldRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static HoldRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "HoldRequest[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "HoldRequest[memo_id]" has a null value in JSON.');
        return true;
      }());

      return HoldRequest(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        reason: mapValueOfType<String>(json, r'reason'),
      );
    }
    return null;
  }

  static List<HoldRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <HoldRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = HoldRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, HoldRequest> mapFromJson(dynamic json) {
    final map = <String, HoldRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = HoldRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of HoldRequest-objects as value to a dart map
  static Map<String, List<HoldRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<HoldRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = HoldRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
  };
}

