//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Generated {
  /// Returns a new [Generated] instance.
  Generated({
    required this.tier,
    required this.source_,
    this.ref,
    this.generatedAt,
    required this.regenerable,
    required this.notice,
  });

  GeneratedTierEnum tier;

  /// Who regenerates it. `serv` is construct-server's wiki generator; `chronicle` is Chronicle deriving from its own corpus — the Scribe's proposals, and the link graph a note's backlinks are read from. 
  GeneratedSource_Enum source_;

  /// The build that generated it — a construct-server commit for `serv`. Absent when the source stamps nothing. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? ref;

  /// Absent when the source stamps nothing. Never invented.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? generatedAt;

  bool regenerable;

  /// The line the tier-1 pane renders — *\"Regenerated from SERV. Separate store — nothing here can overwrite tier 2.\"* for `serv`, and its Chronicle counterpart for `chronicle`. 
  String notice;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Generated &&
    other.tier == tier &&
    other.source_ == source_ &&
    other.ref == ref &&
    other.generatedAt == generatedAt &&
    other.regenerable == regenerable &&
    other.notice == notice;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (tier.hashCode) +
    (source_.hashCode) +
    (ref == null ? 0 : ref!.hashCode) +
    (generatedAt == null ? 0 : generatedAt!.hashCode) +
    (regenerable.hashCode) +
    (notice.hashCode);

  @override
  String toString() => 'Generated[tier=$tier, source_=$source_, ref=$ref, generatedAt=$generatedAt, regenerable=$regenerable, notice=$notice]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'tier'] = this.tier;
      json[r'source'] = this.source_;
    if (this.ref != null) {
      json[r'ref'] = this.ref;
    } else {
      json[r'ref'] = null;
    }
    if (this.generatedAt != null) {
      json[r'generated_at'] = this.generatedAt!.toUtc().toIso8601String();
    } else {
      json[r'generated_at'] = null;
    }
      json[r'regenerable'] = this.regenerable;
      json[r'notice'] = this.notice;
    return json;
  }

  /// Returns a new [Generated] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Generated? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'tier'), 'Required key "Generated[tier]" is missing from JSON.');
        assert(json[r'tier'] != null, 'Required key "Generated[tier]" has a null value in JSON.');
        assert(json.containsKey(r'source'), 'Required key "Generated[source]" is missing from JSON.');
        assert(json[r'source'] != null, 'Required key "Generated[source]" has a null value in JSON.');
        assert(json.containsKey(r'regenerable'), 'Required key "Generated[regenerable]" is missing from JSON.');
        assert(json[r'regenerable'] != null, 'Required key "Generated[regenerable]" has a null value in JSON.');
        assert(json.containsKey(r'notice'), 'Required key "Generated[notice]" is missing from JSON.');
        assert(json[r'notice'] != null, 'Required key "Generated[notice]" has a null value in JSON.');
        return true;
      }());

      return Generated(
        tier: GeneratedTierEnum.fromJson(json[r'tier'])!,
        source_: GeneratedSource_Enum.fromJson(json[r'source'])!,
        ref: mapValueOfType<String>(json, r'ref'),
        generatedAt: mapDateTime(json, r'generated_at', r''),
        regenerable: mapValueOfType<bool>(json, r'regenerable')!,
        notice: mapValueOfType<String>(json, r'notice')!,
      );
    }
    return null;
  }

  static List<Generated> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Generated>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Generated.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Generated> mapFromJson(dynamic json) {
    final map = <String, Generated>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Generated.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Generated-objects as value to a dart map
  static Map<String, List<Generated>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Generated>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Generated.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'tier',
    'source',
    'regenerable',
    'notice',
  };
}


class GeneratedTierEnum {
  /// Instantiate a new enum with the provided [value].
  const GeneratedTierEnum._(this.value);

  /// The underlying value of this enum member.
  final int value;

  @override
  String toString() => value.toString();

  int toJson() => value;

  static const GeneratedTierOne = GeneratedTierEnum._(1);

  /// List of all possible values in this [enum][GeneratedTierEnum].
  static const values = <GeneratedTierEnum>[
    GeneratedTierOne,
  ];

  static GeneratedTierEnum? fromJson(dynamic value) => GeneratedTierEnumTypeTransformer().decode(value);

  static List<GeneratedTierEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <GeneratedTierEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = GeneratedTierEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [GeneratedTierEnum] to int,
/// and [decode] dynamic data back to [GeneratedTierEnum].
class GeneratedTierEnumTypeTransformer {
  factory GeneratedTierEnumTypeTransformer() => _instance ??= const GeneratedTierEnumTypeTransformer._();

  const GeneratedTierEnumTypeTransformer._();

  int encode(GeneratedTierEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a GeneratedTierEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  GeneratedTierEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case 1: return GeneratedTierEnum.GeneratedTierOne;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [GeneratedTierEnumTypeTransformer] instance.
  static GeneratedTierEnumTypeTransformer? _instance;
}


/// Who regenerates it. `serv` is construct-server's wiki generator; `chronicle` is Chronicle deriving from its own corpus — the Scribe's proposals, and the link graph a note's backlinks are read from. 
class GeneratedSource_Enum {
  /// Instantiate a new enum with the provided [value].
  const GeneratedSource_Enum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const serv = GeneratedSource_Enum._(r'serv');
  static const chronicle = GeneratedSource_Enum._(r'chronicle');

  /// List of all possible values in this [enum][GeneratedSource_Enum].
  static const values = <GeneratedSource_Enum>[
    serv,
    chronicle,
  ];

  static GeneratedSource_Enum? fromJson(dynamic value) => GeneratedSource_EnumTypeTransformer().decode(value);

  static List<GeneratedSource_Enum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <GeneratedSource_Enum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = GeneratedSource_Enum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [GeneratedSource_Enum] to String,
/// and [decode] dynamic data back to [GeneratedSource_Enum].
class GeneratedSource_EnumTypeTransformer {
  factory GeneratedSource_EnumTypeTransformer() => _instance ??= const GeneratedSource_EnumTypeTransformer._();

  const GeneratedSource_EnumTypeTransformer._();

  String encode(GeneratedSource_Enum data) => data.value;

  /// Decodes a [dynamic value][data] to a GeneratedSource_Enum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  GeneratedSource_Enum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'serv': return GeneratedSource_Enum.serv;
        case r'chronicle': return GeneratedSource_Enum.chronicle;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [GeneratedSource_EnumTypeTransformer] instance.
  static GeneratedSource_EnumTypeTransformer? _instance;
}


