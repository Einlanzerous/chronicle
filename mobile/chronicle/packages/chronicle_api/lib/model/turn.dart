//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Turn {
  /// Returns a new [Turn] instance.
  Turn({
    required this.id,
    required this.seq,
    required this.authorId,
    required this.authorKind,
    required this.body,
    required this.html,
    this.references = const [],
    required this.createdAt,
    this.composedAt,
    this.memoId,
  });

  String id;

  /// Server-assigned under the thread's row lock. 1 is the opening turn.
  int seq;

  String authorId;

  /// The author's kind WHEN THE TURN WAS WRITTEN, frozen on the row so a later account edit cannot rewrite who said what.
  TurnAuthorKindEnum authorKind;

  /// Markdown, raw.
  String body;

  /// The body rendered, references marked.
  String html;

  /// The estate references the body names, as descriptors. Resolve with `POST /references/resolve`.
  List<ReferenceDescriptor> references;

  /// Arrival, at the server.
  DateTime createdAt;

  /// The client's claim about when it was written. Advisory; never sorted on.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? composedAt;

  /// The memo this turn came from, when it came from one.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? memoId;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Turn &&
    other.id == id &&
    other.seq == seq &&
    other.authorId == authorId &&
    other.authorKind == authorKind &&
    other.body == body &&
    other.html == html &&
    _deepEquality.equals(other.references, references) &&
    other.createdAt == createdAt &&
    other.composedAt == composedAt &&
    other.memoId == memoId;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (seq.hashCode) +
    (authorId.hashCode) +
    (authorKind.hashCode) +
    (body.hashCode) +
    (html.hashCode) +
    (references.hashCode) +
    (createdAt.hashCode) +
    (composedAt == null ? 0 : composedAt!.hashCode) +
    (memoId == null ? 0 : memoId!.hashCode);

  @override
  String toString() => 'Turn[id=$id, seq=$seq, authorId=$authorId, authorKind=$authorKind, body=$body, html=$html, references=$references, createdAt=$createdAt, composedAt=$composedAt, memoId=$memoId]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'seq'] = this.seq;
      json[r'author_id'] = this.authorId;
      json[r'author_kind'] = this.authorKind;
      json[r'body'] = this.body;
      json[r'html'] = this.html;
      json[r'references'] = this.references;
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
    if (this.composedAt != null) {
      json[r'composed_at'] = this.composedAt!.toUtc().toIso8601String();
    } else {
      json[r'composed_at'] = null;
    }
    if (this.memoId != null) {
      json[r'memo_id'] = this.memoId;
    } else {
      json[r'memo_id'] = null;
    }
    return json;
  }

  /// Returns a new [Turn] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Turn? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "Turn[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "Turn[id]" has a null value in JSON.');
        assert(json.containsKey(r'seq'), 'Required key "Turn[seq]" is missing from JSON.');
        assert(json[r'seq'] != null, 'Required key "Turn[seq]" has a null value in JSON.');
        assert(json.containsKey(r'author_id'), 'Required key "Turn[author_id]" is missing from JSON.');
        assert(json[r'author_id'] != null, 'Required key "Turn[author_id]" has a null value in JSON.');
        assert(json.containsKey(r'author_kind'), 'Required key "Turn[author_kind]" is missing from JSON.');
        assert(json[r'author_kind'] != null, 'Required key "Turn[author_kind]" has a null value in JSON.');
        assert(json.containsKey(r'body'), 'Required key "Turn[body]" is missing from JSON.');
        assert(json[r'body'] != null, 'Required key "Turn[body]" has a null value in JSON.');
        assert(json.containsKey(r'html'), 'Required key "Turn[html]" is missing from JSON.');
        assert(json[r'html'] != null, 'Required key "Turn[html]" has a null value in JSON.');
        assert(json.containsKey(r'references'), 'Required key "Turn[references]" is missing from JSON.');
        assert(json[r'references'] != null, 'Required key "Turn[references]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "Turn[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "Turn[created_at]" has a null value in JSON.');
        return true;
      }());

      return Turn(
        id: mapValueOfType<String>(json, r'id')!,
        seq: mapValueOfType<int>(json, r'seq')!,
        authorId: mapValueOfType<String>(json, r'author_id')!,
        authorKind: TurnAuthorKindEnum.fromJson(json[r'author_kind'])!,
        body: mapValueOfType<String>(json, r'body')!,
        html: mapValueOfType<String>(json, r'html')!,
        references: ReferenceDescriptor.listFromJson(json[r'references']),
        createdAt: mapDateTime(json, r'created_at', r'')!,
        composedAt: mapDateTime(json, r'composed_at', r''),
        memoId: mapValueOfType<String>(json, r'memo_id'),
      );
    }
    return null;
  }

  static List<Turn> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Turn>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Turn.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Turn> mapFromJson(dynamic json) {
    final map = <String, Turn>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Turn.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Turn-objects as value to a dart map
  static Map<String, List<Turn>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Turn>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Turn.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'seq',
    'author_id',
    'author_kind',
    'body',
    'html',
    'references',
    'created_at',
  };
}

/// The author's kind WHEN THE TURN WAS WRITTEN, frozen on the row so a later account edit cannot rewrite who said what.
class TurnAuthorKindEnum {
  /// Instantiate a new enum with the provided [value].
  const TurnAuthorKindEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const person = TurnAuthorKindEnum._(r'person');
  static const agent = TurnAuthorKindEnum._(r'agent');

  /// List of all possible values in this [enum][TurnAuthorKindEnum].
  static const values = <TurnAuthorKindEnum>[
    person,
    agent,
  ];

  static TurnAuthorKindEnum? fromJson(dynamic value) => TurnAuthorKindEnumTypeTransformer().decode(value);

  static List<TurnAuthorKindEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TurnAuthorKindEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TurnAuthorKindEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [TurnAuthorKindEnum] to String,
/// and [decode] dynamic data back to [TurnAuthorKindEnum].
class TurnAuthorKindEnumTypeTransformer {
  factory TurnAuthorKindEnumTypeTransformer() => _instance ??= const TurnAuthorKindEnumTypeTransformer._();

  const TurnAuthorKindEnumTypeTransformer._();

  String encode(TurnAuthorKindEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a TurnAuthorKindEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  TurnAuthorKindEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'person': return TurnAuthorKindEnum.person;
        case r'agent': return TurnAuthorKindEnum.agent;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [TurnAuthorKindEnumTypeTransformer] instance.
  static TurnAuthorKindEnumTypeTransformer? _instance;
}


