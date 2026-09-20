//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ReferenceDescriptor {
  /// Returns a new [ReferenceDescriptor] instance.
  ReferenceDescriptor({
    required this.system,
    this.key,
    this.target,
    required this.token,
    this.number,
  });

  /// Which upstream owns the namespace. **Colour keys off this and never off the project key** — coral is Switchyard, gold is Amber, anywhere either resolves. With fifteen live project keys a marker keyed on the project would make the estate colour rule a fifteen-way mapping every client has to duplicate. 
  ReferenceDescriptorSystemEnum system;

  /// The project key for `switchyard` (`SWY`, `CHRN`), and `CHR` or `DSC` for `chronicle`. Absent for `amber`, whose citations carry no key. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? key;

  /// What a `chronicle` reference points at. Absent for the other two systems.
  ReferenceDescriptorTargetEnum? target;

  /// The reference exactly as written.
  String token;

  /// The number after the hyphen, for the `KEY-N` forms. Absent for `amber`.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? number;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ReferenceDescriptor &&
    other.system == system &&
    other.key == key &&
    other.target == target &&
    other.token == token &&
    other.number == number;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (system.hashCode) +
    (key == null ? 0 : key!.hashCode) +
    (target == null ? 0 : target!.hashCode) +
    (token.hashCode) +
    (number == null ? 0 : number!.hashCode);

  @override
  String toString() => 'ReferenceDescriptor[system=$system, key=$key, target=$target, token=$token, number=$number]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'system'] = this.system;
    if (this.key != null) {
      json[r'key'] = this.key;
    } else {
      json[r'key'] = null;
    }
    if (this.target != null) {
      json[r'target'] = this.target;
    } else {
      json[r'target'] = null;
    }
      json[r'token'] = this.token;
    if (this.number != null) {
      json[r'number'] = this.number;
    } else {
      json[r'number'] = null;
    }
    return json;
  }

  /// Returns a new [ReferenceDescriptor] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ReferenceDescriptor? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'system'), 'Required key "ReferenceDescriptor[system]" is missing from JSON.');
        assert(json[r'system'] != null, 'Required key "ReferenceDescriptor[system]" has a null value in JSON.');
        assert(json.containsKey(r'token'), 'Required key "ReferenceDescriptor[token]" is missing from JSON.');
        assert(json[r'token'] != null, 'Required key "ReferenceDescriptor[token]" has a null value in JSON.');
        return true;
      }());

      return ReferenceDescriptor(
        system: ReferenceDescriptorSystemEnum.fromJson(json[r'system'])!,
        key: mapValueOfType<String>(json, r'key'),
        target: ReferenceDescriptorTargetEnum.fromJson(json[r'target']),
        token: mapValueOfType<String>(json, r'token')!,
        number: mapValueOfType<int>(json, r'number'),
      );
    }
    return null;
  }

  static List<ReferenceDescriptor> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ReferenceDescriptor>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ReferenceDescriptor.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ReferenceDescriptor> mapFromJson(dynamic json) {
    final map = <String, ReferenceDescriptor>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ReferenceDescriptor.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ReferenceDescriptor-objects as value to a dart map
  static Map<String, List<ReferenceDescriptor>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ReferenceDescriptor>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ReferenceDescriptor.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'system',
    'token',
  };
}

/// Which upstream owns the namespace. **Colour keys off this and never off the project key** — coral is Switchyard, gold is Amber, anywhere either resolves. With fifteen live project keys a marker keyed on the project would make the estate colour rule a fifteen-way mapping every client has to duplicate. 
class ReferenceDescriptorSystemEnum {
  /// Instantiate a new enum with the provided [value].
  const ReferenceDescriptorSystemEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const switchyard = ReferenceDescriptorSystemEnum._(r'switchyard');
  static const amber = ReferenceDescriptorSystemEnum._(r'amber');
  static const chronicle = ReferenceDescriptorSystemEnum._(r'chronicle');

  /// List of all possible values in this [enum][ReferenceDescriptorSystemEnum].
  static const values = <ReferenceDescriptorSystemEnum>[
    switchyard,
    amber,
    chronicle,
  ];

  static ReferenceDescriptorSystemEnum? fromJson(dynamic value) => ReferenceDescriptorSystemEnumTypeTransformer().decode(value);

  static List<ReferenceDescriptorSystemEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ReferenceDescriptorSystemEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ReferenceDescriptorSystemEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [ReferenceDescriptorSystemEnum] to String,
/// and [decode] dynamic data back to [ReferenceDescriptorSystemEnum].
class ReferenceDescriptorSystemEnumTypeTransformer {
  factory ReferenceDescriptorSystemEnumTypeTransformer() => _instance ??= const ReferenceDescriptorSystemEnumTypeTransformer._();

  const ReferenceDescriptorSystemEnumTypeTransformer._();

  String encode(ReferenceDescriptorSystemEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a ReferenceDescriptorSystemEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  ReferenceDescriptorSystemEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'switchyard': return ReferenceDescriptorSystemEnum.switchyard;
        case r'amber': return ReferenceDescriptorSystemEnum.amber;
        case r'chronicle': return ReferenceDescriptorSystemEnum.chronicle;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [ReferenceDescriptorSystemEnumTypeTransformer] instance.
  static ReferenceDescriptorSystemEnumTypeTransformer? _instance;
}


/// What a `chronicle` reference points at. Absent for the other two systems.
class ReferenceDescriptorTargetEnum {
  /// Instantiate a new enum with the provided [value].
  const ReferenceDescriptorTargetEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const note = ReferenceDescriptorTargetEnum._(r'note');
  static const discussion = ReferenceDescriptorTargetEnum._(r'discussion');

  /// List of all possible values in this [enum][ReferenceDescriptorTargetEnum].
  static const values = <ReferenceDescriptorTargetEnum>[
    note,
    discussion,
  ];

  static ReferenceDescriptorTargetEnum? fromJson(dynamic value) => ReferenceDescriptorTargetEnumTypeTransformer().decode(value);

  static List<ReferenceDescriptorTargetEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ReferenceDescriptorTargetEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ReferenceDescriptorTargetEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [ReferenceDescriptorTargetEnum] to String,
/// and [decode] dynamic data back to [ReferenceDescriptorTargetEnum].
class ReferenceDescriptorTargetEnumTypeTransformer {
  factory ReferenceDescriptorTargetEnumTypeTransformer() => _instance ??= const ReferenceDescriptorTargetEnumTypeTransformer._();

  const ReferenceDescriptorTargetEnumTypeTransformer._();

  String encode(ReferenceDescriptorTargetEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a ReferenceDescriptorTargetEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  ReferenceDescriptorTargetEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'note': return ReferenceDescriptorTargetEnum.note;
        case r'discussion': return ReferenceDescriptorTargetEnum.discussion;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [ReferenceDescriptorTargetEnumTypeTransformer] instance.
  static ReferenceDescriptorTargetEnumTypeTransformer? _instance;
}


