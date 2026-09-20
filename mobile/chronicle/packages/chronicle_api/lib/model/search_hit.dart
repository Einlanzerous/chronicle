//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class SearchHit {
  /// Returns a new [SearchHit] instance.
  SearchHit({
    required this.kind,
    this.ref,
    this.title,
    this.memoId,
    this.model,
    required this.snippet,
    required this.rank,
    required this.createdAt,
  });

  /// Authored versus transcribed, which is the distinction that is not cosmetic.
  SearchHitKindEnum kind;

  /// `CHR-0311`, on a note hit.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? ref;

  /// On a note hit.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? title;

  /// On a transcript hit.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? memoId;

  /// On a transcript hit. Runner-qualified, e.g. `whisper.cpp/small.en`, so the operator can see which decode matched.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? model;

  /// Up to two fragments around the match, fragments joined by ` … `, each match wrapped in a bare `<b>`. Safe to embed: everything but those two tags is HTML-escaped, because a note body is stored raw and the fragment is a slice of it. 
  String snippet;

  double rank;

  DateTime createdAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is SearchHit &&
    other.kind == kind &&
    other.ref == ref &&
    other.title == title &&
    other.memoId == memoId &&
    other.model == model &&
    other.snippet == snippet &&
    other.rank == rank &&
    other.createdAt == createdAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (kind.hashCode) +
    (ref == null ? 0 : ref!.hashCode) +
    (title == null ? 0 : title!.hashCode) +
    (memoId == null ? 0 : memoId!.hashCode) +
    (model == null ? 0 : model!.hashCode) +
    (snippet.hashCode) +
    (rank.hashCode) +
    (createdAt.hashCode);

  @override
  String toString() => 'SearchHit[kind=$kind, ref=$ref, title=$title, memoId=$memoId, model=$model, snippet=$snippet, rank=$rank, createdAt=$createdAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'kind'] = this.kind;
    if (this.ref != null) {
      json[r'ref'] = this.ref;
    } else {
      json[r'ref'] = null;
    }
    if (this.title != null) {
      json[r'title'] = this.title;
    } else {
      json[r'title'] = null;
    }
    if (this.memoId != null) {
      json[r'memo_id'] = this.memoId;
    } else {
      json[r'memo_id'] = null;
    }
    if (this.model != null) {
      json[r'model'] = this.model;
    } else {
      json[r'model'] = null;
    }
      json[r'snippet'] = this.snippet;
      json[r'rank'] = this.rank;
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
    return json;
  }

  /// Returns a new [SearchHit] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static SearchHit? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'kind'), 'Required key "SearchHit[kind]" is missing from JSON.');
        assert(json[r'kind'] != null, 'Required key "SearchHit[kind]" has a null value in JSON.');
        assert(json.containsKey(r'snippet'), 'Required key "SearchHit[snippet]" is missing from JSON.');
        assert(json[r'snippet'] != null, 'Required key "SearchHit[snippet]" has a null value in JSON.');
        assert(json.containsKey(r'rank'), 'Required key "SearchHit[rank]" is missing from JSON.');
        assert(json[r'rank'] != null, 'Required key "SearchHit[rank]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "SearchHit[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "SearchHit[created_at]" has a null value in JSON.');
        return true;
      }());

      return SearchHit(
        kind: SearchHitKindEnum.fromJson(json[r'kind'])!,
        ref: mapValueOfType<String>(json, r'ref'),
        title: mapValueOfType<String>(json, r'title'),
        memoId: mapValueOfType<String>(json, r'memo_id'),
        model: mapValueOfType<String>(json, r'model'),
        snippet: mapValueOfType<String>(json, r'snippet')!,
        rank: mapValueOfType<double>(json, r'rank')!,
        createdAt: mapDateTime(json, r'created_at', r'')!,
      );
    }
    return null;
  }

  static List<SearchHit> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SearchHit>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SearchHit.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, SearchHit> mapFromJson(dynamic json) {
    final map = <String, SearchHit>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = SearchHit.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of SearchHit-objects as value to a dart map
  static Map<String, List<SearchHit>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<SearchHit>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = SearchHit.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'kind',
    'snippet',
    'rank',
    'created_at',
  };
}

/// Authored versus transcribed, which is the distinction that is not cosmetic.
class SearchHitKindEnum {
  /// Instantiate a new enum with the provided [value].
  const SearchHitKindEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const note = SearchHitKindEnum._(r'note');
  static const transcript = SearchHitKindEnum._(r'transcript');

  /// List of all possible values in this [enum][SearchHitKindEnum].
  static const values = <SearchHitKindEnum>[
    note,
    transcript,
  ];

  static SearchHitKindEnum? fromJson(dynamic value) => SearchHitKindEnumTypeTransformer().decode(value);

  static List<SearchHitKindEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <SearchHitKindEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = SearchHitKindEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [SearchHitKindEnum] to String,
/// and [decode] dynamic data back to [SearchHitKindEnum].
class SearchHitKindEnumTypeTransformer {
  factory SearchHitKindEnumTypeTransformer() => _instance ??= const SearchHitKindEnumTypeTransformer._();

  const SearchHitKindEnumTypeTransformer._();

  String encode(SearchHitKindEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a SearchHitKindEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  SearchHitKindEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'note': return SearchHitKindEnum.note;
        case r'transcript': return SearchHitKindEnum.transcript;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [SearchHitKindEnumTypeTransformer] instance.
  static SearchHitKindEnumTypeTransformer? _instance;
}


