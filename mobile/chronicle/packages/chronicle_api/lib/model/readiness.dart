//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Readiness {
  /// Returns a new [Readiness] instance.
  Readiness({
    required this.status,
    this.check,
  });

  ReadinessStatusEnum status;

  /// Which dependency failed. Present only on `unready`, and naming the check rather than the error: the detail belongs in the log, not in a body that may cross the WAN. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? check;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Readiness &&
    other.status == status &&
    other.check == check;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (status.hashCode) +
    (check == null ? 0 : check!.hashCode);

  @override
  String toString() => 'Readiness[status=$status, check=$check]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'status'] = this.status;
    if (this.check != null) {
      json[r'check'] = this.check;
    } else {
      json[r'check'] = null;
    }
    return json;
  }

  /// Returns a new [Readiness] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Readiness? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'status'), 'Required key "Readiness[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "Readiness[status]" has a null value in JSON.');
        return true;
      }());

      return Readiness(
        status: ReadinessStatusEnum.fromJson(json[r'status'])!,
        check: mapValueOfType<String>(json, r'check'),
      );
    }
    return null;
  }

  static List<Readiness> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Readiness>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Readiness.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Readiness> mapFromJson(dynamic json) {
    final map = <String, Readiness>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Readiness.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Readiness-objects as value to a dart map
  static Map<String, List<Readiness>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Readiness>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Readiness.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'status',
  };
}


class ReadinessStatusEnum {
  /// Instantiate a new enum with the provided [value].
  const ReadinessStatusEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const ready = ReadinessStatusEnum._(r'ready');
  static const unready = ReadinessStatusEnum._(r'unready');

  /// List of all possible values in this [enum][ReadinessStatusEnum].
  static const values = <ReadinessStatusEnum>[
    ready,
    unready,
  ];

  static ReadinessStatusEnum? fromJson(dynamic value) => ReadinessStatusEnumTypeTransformer().decode(value);

  static List<ReadinessStatusEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ReadinessStatusEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ReadinessStatusEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [ReadinessStatusEnum] to String,
/// and [decode] dynamic data back to [ReadinessStatusEnum].
class ReadinessStatusEnumTypeTransformer {
  factory ReadinessStatusEnumTypeTransformer() => _instance ??= const ReadinessStatusEnumTypeTransformer._();

  const ReadinessStatusEnumTypeTransformer._();

  String encode(ReadinessStatusEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a ReadinessStatusEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  ReadinessStatusEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'ready': return ReadinessStatusEnum.ready;
        case r'unready': return ReadinessStatusEnum.unready;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [ReadinessStatusEnumTypeTransformer] instance.
  static ReadinessStatusEnumTypeTransformer? _instance;
}


