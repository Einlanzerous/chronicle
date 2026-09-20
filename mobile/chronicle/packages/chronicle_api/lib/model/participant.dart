//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Participant {
  /// Returns a new [Participant] instance.
  Participant({
    required this.userId,
    required this.kind,
    required this.displayName,
    required this.addedAt,
    required this.addedBy,
    this.removedAt,
    this.removedBy,
    this.lastReadSeq,
    this.lastReadAt,
  });

  String userId;

  /// The account's CURRENT kind — membership is current state, unlike a turn's frozen author_kind.
  ParticipantKindEnum kind;

  String displayName;

  /// FIRST added. A re-add after a removal does not reattribute the original invitation.
  DateTime addedAt;

  String addedBy;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? removedAt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? removedBy;

  /// How far they have read. Absent means never read, which is not the same as 0 — and always absent for an agent.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? lastReadSeq;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? lastReadAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Participant &&
    other.userId == userId &&
    other.kind == kind &&
    other.displayName == displayName &&
    other.addedAt == addedAt &&
    other.addedBy == addedBy &&
    other.removedAt == removedAt &&
    other.removedBy == removedBy &&
    other.lastReadSeq == lastReadSeq &&
    other.lastReadAt == lastReadAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (userId.hashCode) +
    (kind.hashCode) +
    (displayName.hashCode) +
    (addedAt.hashCode) +
    (addedBy.hashCode) +
    (removedAt == null ? 0 : removedAt!.hashCode) +
    (removedBy == null ? 0 : removedBy!.hashCode) +
    (lastReadSeq == null ? 0 : lastReadSeq!.hashCode) +
    (lastReadAt == null ? 0 : lastReadAt!.hashCode);

  @override
  String toString() => 'Participant[userId=$userId, kind=$kind, displayName=$displayName, addedAt=$addedAt, addedBy=$addedBy, removedAt=$removedAt, removedBy=$removedBy, lastReadSeq=$lastReadSeq, lastReadAt=$lastReadAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'user_id'] = this.userId;
      json[r'kind'] = this.kind;
      json[r'display_name'] = this.displayName;
      json[r'added_at'] = this.addedAt.toUtc().toIso8601String();
      json[r'added_by'] = this.addedBy;
    if (this.removedAt != null) {
      json[r'removed_at'] = this.removedAt!.toUtc().toIso8601String();
    } else {
      json[r'removed_at'] = null;
    }
    if (this.removedBy != null) {
      json[r'removed_by'] = this.removedBy;
    } else {
      json[r'removed_by'] = null;
    }
    if (this.lastReadSeq != null) {
      json[r'last_read_seq'] = this.lastReadSeq;
    } else {
      json[r'last_read_seq'] = null;
    }
    if (this.lastReadAt != null) {
      json[r'last_read_at'] = this.lastReadAt!.toUtc().toIso8601String();
    } else {
      json[r'last_read_at'] = null;
    }
    return json;
  }

  /// Returns a new [Participant] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Participant? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'user_id'), 'Required key "Participant[user_id]" is missing from JSON.');
        assert(json[r'user_id'] != null, 'Required key "Participant[user_id]" has a null value in JSON.');
        assert(json.containsKey(r'kind'), 'Required key "Participant[kind]" is missing from JSON.');
        assert(json[r'kind'] != null, 'Required key "Participant[kind]" has a null value in JSON.');
        assert(json.containsKey(r'display_name'), 'Required key "Participant[display_name]" is missing from JSON.');
        assert(json[r'display_name'] != null, 'Required key "Participant[display_name]" has a null value in JSON.');
        assert(json.containsKey(r'added_at'), 'Required key "Participant[added_at]" is missing from JSON.');
        assert(json[r'added_at'] != null, 'Required key "Participant[added_at]" has a null value in JSON.');
        assert(json.containsKey(r'added_by'), 'Required key "Participant[added_by]" is missing from JSON.');
        assert(json[r'added_by'] != null, 'Required key "Participant[added_by]" has a null value in JSON.');
        return true;
      }());

      return Participant(
        userId: mapValueOfType<String>(json, r'user_id')!,
        kind: ParticipantKindEnum.fromJson(json[r'kind'])!,
        displayName: mapValueOfType<String>(json, r'display_name')!,
        addedAt: mapDateTime(json, r'added_at', r'')!,
        addedBy: mapValueOfType<String>(json, r'added_by')!,
        removedAt: mapDateTime(json, r'removed_at', r''),
        removedBy: mapValueOfType<String>(json, r'removed_by'),
        lastReadSeq: mapValueOfType<int>(json, r'last_read_seq'),
        lastReadAt: mapDateTime(json, r'last_read_at', r''),
      );
    }
    return null;
  }

  static List<Participant> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Participant>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Participant.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Participant> mapFromJson(dynamic json) {
    final map = <String, Participant>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Participant.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Participant-objects as value to a dart map
  static Map<String, List<Participant>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Participant>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Participant.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'user_id',
    'kind',
    'display_name',
    'added_at',
    'added_by',
  };
}

/// The account's CURRENT kind — membership is current state, unlike a turn's frozen author_kind.
class ParticipantKindEnum {
  /// Instantiate a new enum with the provided [value].
  const ParticipantKindEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const person = ParticipantKindEnum._(r'person');
  static const agent = ParticipantKindEnum._(r'agent');

  /// List of all possible values in this [enum][ParticipantKindEnum].
  static const values = <ParticipantKindEnum>[
    person,
    agent,
  ];

  static ParticipantKindEnum? fromJson(dynamic value) => ParticipantKindEnumTypeTransformer().decode(value);

  static List<ParticipantKindEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ParticipantKindEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ParticipantKindEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [ParticipantKindEnum] to String,
/// and [decode] dynamic data back to [ParticipantKindEnum].
class ParticipantKindEnumTypeTransformer {
  factory ParticipantKindEnumTypeTransformer() => _instance ??= const ParticipantKindEnumTypeTransformer._();

  const ParticipantKindEnumTypeTransformer._();

  String encode(ParticipantKindEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a ParticipantKindEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  ParticipantKindEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'person': return ParticipantKindEnum.person;
        case r'agent': return ParticipantKindEnum.agent;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [ParticipantKindEnumTypeTransformer] instance.
  static ParticipantKindEnumTypeTransformer? _instance;
}


