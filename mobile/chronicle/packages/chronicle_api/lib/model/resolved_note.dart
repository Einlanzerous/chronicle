//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ResolvedNote {
  /// Returns a new [ResolvedNote] instance.
  ResolvedNote({
    required this.ref,
    required this.title,
    required this.page,
    required this.createdAt,
    required this.updatedAt,
    required this.revision,
    this.resolvedFrom = const [],
  });

  /// `CHR-0311`. Permanent: the number is minted once and never reused.
  String ref;

  String title;

  /// The path of the page it is filed on, current as of this read.
  String page;

  DateTime createdAt;

  DateTime updatedAt;

  RevisionMeta revision;

  /// Every thread that concluded into this note, this one included.
  List<DiscussionSummary> resolvedFrom;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ResolvedNote &&
    other.ref == ref &&
    other.title == title &&
    other.page == page &&
    other.createdAt == createdAt &&
    other.updatedAt == updatedAt &&
    other.revision == revision &&
    _deepEquality.equals(other.resolvedFrom, resolvedFrom);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (ref.hashCode) +
    (title.hashCode) +
    (page.hashCode) +
    (createdAt.hashCode) +
    (updatedAt.hashCode) +
    (revision.hashCode) +
    (resolvedFrom.hashCode);

  @override
  String toString() => 'ResolvedNote[ref=$ref, title=$title, page=$page, createdAt=$createdAt, updatedAt=$updatedAt, revision=$revision, resolvedFrom=$resolvedFrom]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'ref'] = this.ref;
      json[r'title'] = this.title;
      json[r'page'] = this.page;
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
      json[r'updated_at'] = this.updatedAt.toUtc().toIso8601String();
      json[r'revision'] = this.revision;
      json[r'resolved_from'] = this.resolvedFrom;
    return json;
  }

  /// Returns a new [ResolvedNote] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ResolvedNote? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'ref'), 'Required key "ResolvedNote[ref]" is missing from JSON.');
        assert(json[r'ref'] != null, 'Required key "ResolvedNote[ref]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "ResolvedNote[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "ResolvedNote[title]" has a null value in JSON.');
        assert(json.containsKey(r'page'), 'Required key "ResolvedNote[page]" is missing from JSON.');
        assert(json[r'page'] != null, 'Required key "ResolvedNote[page]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "ResolvedNote[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "ResolvedNote[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'updated_at'), 'Required key "ResolvedNote[updated_at]" is missing from JSON.');
        assert(json[r'updated_at'] != null, 'Required key "ResolvedNote[updated_at]" has a null value in JSON.');
        assert(json.containsKey(r'revision'), 'Required key "ResolvedNote[revision]" is missing from JSON.');
        assert(json[r'revision'] != null, 'Required key "ResolvedNote[revision]" has a null value in JSON.');
        assert(json.containsKey(r'resolved_from'), 'Required key "ResolvedNote[resolved_from]" is missing from JSON.');
        assert(json[r'resolved_from'] != null, 'Required key "ResolvedNote[resolved_from]" has a null value in JSON.');
        return true;
      }());

      return ResolvedNote(
        ref: mapValueOfType<String>(json, r'ref')!,
        title: mapValueOfType<String>(json, r'title')!,
        page: mapValueOfType<String>(json, r'page')!,
        createdAt: mapDateTime(json, r'created_at', r'')!,
        updatedAt: mapDateTime(json, r'updated_at', r'')!,
        revision: RevisionMeta.fromJson(json[r'revision'])!,
        resolvedFrom: DiscussionSummary.listFromJson(json[r'resolved_from']),
      );
    }
    return null;
  }

  static List<ResolvedNote> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ResolvedNote>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ResolvedNote.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ResolvedNote> mapFromJson(dynamic json) {
    final map = <String, ResolvedNote>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ResolvedNote.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ResolvedNote-objects as value to a dart map
  static Map<String, List<ResolvedNote>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ResolvedNote>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ResolvedNote.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'ref',
    'title',
    'page',
    'created_at',
    'updated_at',
    'revision',
    'resolved_from',
  };
}

