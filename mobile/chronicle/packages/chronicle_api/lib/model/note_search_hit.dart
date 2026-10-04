//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class NoteSearchHit {
  /// Returns a new [NoteSearchHit] instance.
  NoteSearchHit({
    required this.ref,
    this.title,
    required this.snippet,
    required this.rank,
    required this.createdAt,
  });

  /// `CHR-0311`.
  String ref;

  /// Absent when the current revision's title is empty.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? title;

  /// `SearchHit.snippet`'s contract: up to two fragments around the match, joined by ` … `, each match wrapped in a bare `<b>`, and everything but those two tags HTML-escaped. 
  String snippet;

  double rank;

  DateTime createdAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is NoteSearchHit &&
    other.ref == ref &&
    other.title == title &&
    other.snippet == snippet &&
    other.rank == rank &&
    other.createdAt == createdAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (ref.hashCode) +
    (title == null ? 0 : title!.hashCode) +
    (snippet.hashCode) +
    (rank.hashCode) +
    (createdAt.hashCode);

  @override
  String toString() => 'NoteSearchHit[ref=$ref, title=$title, snippet=$snippet, rank=$rank, createdAt=$createdAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'ref'] = this.ref;
    if (this.title != null) {
      json[r'title'] = this.title;
    } else {
      json[r'title'] = null;
    }
      json[r'snippet'] = this.snippet;
      json[r'rank'] = this.rank;
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
    return json;
  }

  /// Returns a new [NoteSearchHit] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static NoteSearchHit? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'ref'), 'Required key "NoteSearchHit[ref]" is missing from JSON.');
        assert(json[r'ref'] != null, 'Required key "NoteSearchHit[ref]" has a null value in JSON.');
        assert(json.containsKey(r'snippet'), 'Required key "NoteSearchHit[snippet]" is missing from JSON.');
        assert(json[r'snippet'] != null, 'Required key "NoteSearchHit[snippet]" has a null value in JSON.');
        assert(json.containsKey(r'rank'), 'Required key "NoteSearchHit[rank]" is missing from JSON.');
        assert(json[r'rank'] != null, 'Required key "NoteSearchHit[rank]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "NoteSearchHit[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "NoteSearchHit[created_at]" has a null value in JSON.');
        return true;
      }());

      return NoteSearchHit(
        ref: mapValueOfType<String>(json, r'ref')!,
        title: mapValueOfType<String>(json, r'title'),
        snippet: mapValueOfType<String>(json, r'snippet')!,
        rank: mapValueOfType<double>(json, r'rank')!,
        createdAt: mapDateTime(json, r'created_at', r'')!,
      );
    }
    return null;
  }

  static List<NoteSearchHit> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <NoteSearchHit>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = NoteSearchHit.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, NoteSearchHit> mapFromJson(dynamic json) {
    final map = <String, NoteSearchHit>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = NoteSearchHit.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of NoteSearchHit-objects as value to a dart map
  static Map<String, List<NoteSearchHit>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<NoteSearchHit>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = NoteSearchHit.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'ref',
    'snippet',
    'rank',
    'created_at',
  };
}

