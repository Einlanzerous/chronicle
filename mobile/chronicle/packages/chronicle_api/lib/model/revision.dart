//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Revision {
  /// Returns a new [Revision] instance.
  Revision({
    required this.id,
    required this.seq,
    required this.createdAt,
    required this.authorId,
    this.confirmedBy,
    this.verb,
    this.memoId,
    this.restoredFrom,
    required this.title,
    required this.body,
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
  RevisionVerbEnum? verb;

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

  String title;

  String body;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Revision &&
    other.id == id &&
    other.seq == seq &&
    other.createdAt == createdAt &&
    other.authorId == authorId &&
    other.confirmedBy == confirmedBy &&
    other.verb == verb &&
    other.memoId == memoId &&
    other.restoredFrom == restoredFrom &&
    other.title == title &&
    other.body == body;

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
    (restoredFrom == null ? 0 : restoredFrom!.hashCode) +
    (title.hashCode) +
    (body.hashCode);

  @override
  String toString() => 'Revision[id=$id, seq=$seq, createdAt=$createdAt, authorId=$authorId, confirmedBy=$confirmedBy, verb=$verb, memoId=$memoId, restoredFrom=$restoredFrom, title=$title, body=$body]';

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
      json[r'title'] = this.title;
      json[r'body'] = this.body;
    return json;
  }

  /// Returns a new [Revision] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Revision? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "Revision[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "Revision[id]" has a null value in JSON.');
        assert(json.containsKey(r'seq'), 'Required key "Revision[seq]" is missing from JSON.');
        assert(json[r'seq'] != null, 'Required key "Revision[seq]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "Revision[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "Revision[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'author_id'), 'Required key "Revision[author_id]" is missing from JSON.');
        assert(json[r'author_id'] != null, 'Required key "Revision[author_id]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "Revision[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "Revision[title]" has a null value in JSON.');
        assert(json.containsKey(r'body'), 'Required key "Revision[body]" is missing from JSON.');
        assert(json[r'body'] != null, 'Required key "Revision[body]" has a null value in JSON.');
        return true;
      }());

      return Revision(
        id: mapValueOfType<String>(json, r'id')!,
        seq: mapValueOfType<int>(json, r'seq')!,
        createdAt: mapDateTime(json, r'created_at', r'')!,
        authorId: mapValueOfType<String>(json, r'author_id')!,
        confirmedBy: mapValueOfType<String>(json, r'confirmed_by'),
        verb: RevisionVerbEnum.fromJson(json[r'verb']),
        memoId: mapValueOfType<String>(json, r'memo_id'),
        restoredFrom: mapValueOfType<String>(json, r'restored_from'),
        title: mapValueOfType<String>(json, r'title')!,
        body: mapValueOfType<String>(json, r'body')!,
      );
    }
    return null;
  }

  static List<Revision> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Revision>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Revision.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Revision> mapFromJson(dynamic json) {
    final map = <String, Revision>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Revision.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Revision-objects as value to a dart map
  static Map<String, List<Revision>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Revision>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Revision.listFromJson(entry.value, growable: growable,);
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
    'title',
    'body',
  };
}

/// What a person confirmed about a Scribe proposal. Absent when somebody typed the text directly.
class RevisionVerbEnum {
  /// Instantiate a new enum with the provided [value].
  const RevisionVerbEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const create = RevisionVerbEnum._(r'create');
  static const append = RevisionVerbEnum._(r'append');
  static const supersede = RevisionVerbEnum._(r'supersede');
  static const relate = RevisionVerbEnum._(r'relate');

  /// List of all possible values in this [enum][RevisionVerbEnum].
  static const values = <RevisionVerbEnum>[
    create,
    append,
    supersede,
    relate,
  ];

  static RevisionVerbEnum? fromJson(dynamic value) => RevisionVerbEnumTypeTransformer().decode(value);

  static List<RevisionVerbEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RevisionVerbEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RevisionVerbEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [RevisionVerbEnum] to String,
/// and [decode] dynamic data back to [RevisionVerbEnum].
class RevisionVerbEnumTypeTransformer {
  factory RevisionVerbEnumTypeTransformer() => _instance ??= const RevisionVerbEnumTypeTransformer._();

  const RevisionVerbEnumTypeTransformer._();

  String encode(RevisionVerbEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a RevisionVerbEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  RevisionVerbEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'create': return RevisionVerbEnum.create;
        case r'append': return RevisionVerbEnum.append;
        case r'supersede': return RevisionVerbEnum.supersede;
        case r'relate': return RevisionVerbEnum.relate;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [RevisionVerbEnumTypeTransformer] instance.
  static RevisionVerbEnumTypeTransformer? _instance;
}


