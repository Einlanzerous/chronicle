//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class RevisionMeta {
  /// Returns a new [RevisionMeta] instance.
  RevisionMeta({
    required this.id,
    required this.seq,
    required this.createdAt,
    required this.authorId,
    this.confirmedBy,
    this.verb,
    this.memoId,
    this.restoredFrom,
  });

  String id;

  /// 1 for the first revision. Only ever grows; a restore appends.
  int seq;

  DateTime createdAt;

  /// Whose words these are. May be an agent, for a Scribe-routed memo.
  String authorId;

  /// Who agreed to this text landing — never an agent. A different question from `author_id`. Absent only on rows written before the guard existed. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? confirmedBy;

  /// What a person confirmed about a Scribe proposal. Absent when somebody typed the text directly.
  RevisionMetaVerbEnum? verb;

  /// The memo this text came from, when it came from one.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? memoId;

  /// The revision this one reproduces, when it is a restore.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? restoredFrom;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RevisionMeta &&
    other.id == id &&
    other.seq == seq &&
    other.createdAt == createdAt &&
    other.authorId == authorId &&
    other.confirmedBy == confirmedBy &&
    other.verb == verb &&
    other.memoId == memoId &&
    other.restoredFrom == restoredFrom;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (seq.hashCode) +
    (createdAt.hashCode) +
    (authorId.hashCode) +
    (confirmedBy == null ? 0 : confirmedBy!.hashCode) +
    (verb == null ? 0 : verb!.hashCode) +
    (memoId == null ? 0 : memoId!.hashCode) +
    (restoredFrom == null ? 0 : restoredFrom!.hashCode);

  @override
  String toString() => 'RevisionMeta[id=$id, seq=$seq, createdAt=$createdAt, authorId=$authorId, confirmedBy=$confirmedBy, verb=$verb, memoId=$memoId, restoredFrom=$restoredFrom]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'seq'] = this.seq;
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
      json[r'author_id'] = this.authorId;
    if (this.confirmedBy != null) {
      json[r'confirmed_by'] = this.confirmedBy;
    } else {
      json[r'confirmed_by'] = null;
    }
    if (this.verb != null) {
      json[r'verb'] = this.verb;
    } else {
      json[r'verb'] = null;
    }
    if (this.memoId != null) {
      json[r'memo_id'] = this.memoId;
    } else {
      json[r'memo_id'] = null;
    }
    if (this.restoredFrom != null) {
      json[r'restored_from'] = this.restoredFrom;
    } else {
      json[r'restored_from'] = null;
    }
    return json;
  }

  /// Returns a new [RevisionMeta] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RevisionMeta? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "RevisionMeta[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "RevisionMeta[id]" has a null value in JSON.');
        assert(json.containsKey(r'seq'), 'Required key "RevisionMeta[seq]" is missing from JSON.');
        assert(json[r'seq'] != null, 'Required key "RevisionMeta[seq]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "RevisionMeta[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "RevisionMeta[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'author_id'), 'Required key "RevisionMeta[author_id]" is missing from JSON.');
        assert(json[r'author_id'] != null, 'Required key "RevisionMeta[author_id]" has a null value in JSON.');
        return true;
      }());

      return RevisionMeta(
        id: mapValueOfType<String>(json, r'id')!,
        seq: mapValueOfType<int>(json, r'seq')!,
        createdAt: mapDateTime(json, r'created_at', r'')!,
        authorId: mapValueOfType<String>(json, r'author_id')!,
        confirmedBy: mapValueOfType<String>(json, r'confirmed_by'),
        verb: RevisionMetaVerbEnum.fromJson(json[r'verb']),
        memoId: mapValueOfType<String>(json, r'memo_id'),
        restoredFrom: mapValueOfType<String>(json, r'restored_from'),
      );
    }
    return null;
  }

  static List<RevisionMeta> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RevisionMeta>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RevisionMeta.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RevisionMeta> mapFromJson(dynamic json) {
    final map = <String, RevisionMeta>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RevisionMeta.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RevisionMeta-objects as value to a dart map
  static Map<String, List<RevisionMeta>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RevisionMeta>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RevisionMeta.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'seq',
    'created_at',
    'author_id',
  };
}

/// What a person confirmed about a Scribe proposal. Absent when somebody typed the text directly.
class RevisionMetaVerbEnum {
  /// Instantiate a new enum with the provided [value].
  const RevisionMetaVerbEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const create = RevisionMetaVerbEnum._(r'create');
  static const append = RevisionMetaVerbEnum._(r'append');
  static const supersede = RevisionMetaVerbEnum._(r'supersede');
  static const relate = RevisionMetaVerbEnum._(r'relate');

  /// List of all possible values in this [enum][RevisionMetaVerbEnum].
  static const values = <RevisionMetaVerbEnum>[
    create,
    append,
    supersede,
    relate,
  ];

  static RevisionMetaVerbEnum? fromJson(dynamic value) => RevisionMetaVerbEnumTypeTransformer().decode(value);

  static List<RevisionMetaVerbEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RevisionMetaVerbEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RevisionMetaVerbEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [RevisionMetaVerbEnum] to String,
/// and [decode] dynamic data back to [RevisionMetaVerbEnum].
class RevisionMetaVerbEnumTypeTransformer {
  factory RevisionMetaVerbEnumTypeTransformer() => _instance ??= const RevisionMetaVerbEnumTypeTransformer._();

  const RevisionMetaVerbEnumTypeTransformer._();

  String encode(RevisionMetaVerbEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a RevisionMetaVerbEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  RevisionMetaVerbEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'create': return RevisionMetaVerbEnum.create;
        case r'append': return RevisionMetaVerbEnum.append;
        case r'supersede': return RevisionMetaVerbEnum.supersede;
        case r'relate': return RevisionMetaVerbEnum.relate;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [RevisionMetaVerbEnumTypeTransformer] instance.
  static RevisionMetaVerbEnumTypeTransformer? _instance;
}


