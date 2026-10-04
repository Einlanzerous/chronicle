//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class RaiseRetentionRequest {
  /// Returns a new [RaiseRetentionRequest] instance.
  RaiseRetentionRequest({
    required this.retention,
  });

  /// The level to raise the memo to. `forever` is the pin. All three levels are accepted so that a request to LOWER is recognised and refused `409` with its reason, rather than bounced as a malformed body that never says why. 
  RaiseRetentionRequestRetentionEnum retention;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RaiseRetentionRequest &&
    other.retention == retention;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (retention.hashCode);

  @override
  String toString() => 'RaiseRetentionRequest[retention=$retention]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'retention'] = this.retention;
    return json;
  }

  /// Returns a new [RaiseRetentionRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RaiseRetentionRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'retention'), 'Required key "RaiseRetentionRequest[retention]" is missing from JSON.');
        assert(json[r'retention'] != null, 'Required key "RaiseRetentionRequest[retention]" has a null value in JSON.');
        return true;
      }());

      return RaiseRetentionRequest(
        retention: RaiseRetentionRequestRetentionEnum.fromJson(json[r'retention'])!,
      );
    }
    return null;
  }

  static List<RaiseRetentionRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RaiseRetentionRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RaiseRetentionRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RaiseRetentionRequest> mapFromJson(dynamic json) {
    final map = <String, RaiseRetentionRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RaiseRetentionRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RaiseRetentionRequest-objects as value to a dart map
  static Map<String, List<RaiseRetentionRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RaiseRetentionRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RaiseRetentionRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'retention',
  };
}

/// The level to raise the memo to. `forever` is the pin. All three levels are accepted so that a request to LOWER is recognised and refused `409` with its reason, rather than bounced as a malformed body that never says why. 
class RaiseRetentionRequestRetentionEnum {
  /// Instantiate a new enum with the provided [value].
  const RaiseRetentionRequestRetentionEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const discardNow = RaiseRetentionRequestRetentionEnum._(r'discard_now');
  static const days30 = RaiseRetentionRequestRetentionEnum._(r'days_30');
  static const forever = RaiseRetentionRequestRetentionEnum._(r'forever');

  /// List of all possible values in this [enum][RaiseRetentionRequestRetentionEnum].
  static const values = <RaiseRetentionRequestRetentionEnum>[
    discardNow,
    days30,
    forever,
  ];

  static RaiseRetentionRequestRetentionEnum? fromJson(dynamic value) => RaiseRetentionRequestRetentionEnumTypeTransformer().decode(value);

  static List<RaiseRetentionRequestRetentionEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RaiseRetentionRequestRetentionEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RaiseRetentionRequestRetentionEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [RaiseRetentionRequestRetentionEnum] to String,
/// and [decode] dynamic data back to [RaiseRetentionRequestRetentionEnum].
class RaiseRetentionRequestRetentionEnumTypeTransformer {
  factory RaiseRetentionRequestRetentionEnumTypeTransformer() => _instance ??= const RaiseRetentionRequestRetentionEnumTypeTransformer._();

  const RaiseRetentionRequestRetentionEnumTypeTransformer._();

  String encode(RaiseRetentionRequestRetentionEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a RaiseRetentionRequestRetentionEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  RaiseRetentionRequestRetentionEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'discard_now': return RaiseRetentionRequestRetentionEnum.discardNow;
        case r'days_30': return RaiseRetentionRequestRetentionEnum.days30;
        case r'forever': return RaiseRetentionRequestRetentionEnum.forever;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [RaiseRetentionRequestRetentionEnumTypeTransformer] instance.
  static RaiseRetentionRequestRetentionEnumTypeTransformer? _instance;
}


