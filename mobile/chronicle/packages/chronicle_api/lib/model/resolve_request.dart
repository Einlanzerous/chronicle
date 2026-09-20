//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ResolveRequest {
  /// Returns a new [ResolveRequest] instance.
  ResolveRequest({
    this.references = const [],
  });

  /// At most 50 — CHRN-51's cap, which counts attempts per render. The bound is enforced server-side as well as declared, and a larger batch is refused rather than truncated: a client that sent sixty and got fifty back would hold ten cards it could never explain. 
  List<ReferenceDescriptor> references;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ResolveRequest &&
    _deepEquality.equals(other.references, references);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (references.hashCode);

  @override
  String toString() => 'ResolveRequest[references=$references]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'references'] = this.references;
    return json;
  }

  /// Returns a new [ResolveRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ResolveRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'references'), 'Required key "ResolveRequest[references]" is missing from JSON.');
        assert(json[r'references'] != null, 'Required key "ResolveRequest[references]" has a null value in JSON.');
        return true;
      }());

      return ResolveRequest(
        references: ReferenceDescriptor.listFromJson(json[r'references']),
      );
    }
    return null;
  }

  static List<ResolveRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ResolveRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ResolveRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ResolveRequest> mapFromJson(dynamic json) {
    final map = <String, ResolveRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ResolveRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ResolveRequest-objects as value to a dart map
  static Map<String, List<ResolveRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ResolveRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ResolveRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'references',
  };
}

