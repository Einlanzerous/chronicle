//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Note {
  /// Returns a new [Note] instance.
  Note({
    required this.ref,
    required this.title,
    required this.page,
    required this.createdAt,
    required this.updatedAt,
    required this.body,
    required this.html,
    this.references = const [],
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

  /// The current revision's markdown, raw, for an editor.
  String body;

  String html;

  /// In order of appearance; a token named three times appears three times.
  List<ReferenceDescriptor> references;

  RevisionMeta revision;

  /// The discussions that concluded into this note — the reverse half of \"linked both ways\" (CHRN-46), read from `tier2.discussions` rather than stored on the note, because a note may be what several threads concluded. Empty when none. 
  List<DiscussionSummary> resolvedFrom;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Note &&
    other.ref == ref &&
    other.title == title &&
    other.page == page &&
    other.createdAt == createdAt &&
    other.updatedAt == updatedAt &&
    other.body == body &&
    other.html == html &&
    _deepEquality.equals(other.references, references) &&
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
    (body.hashCode) +
    (html.hashCode) +
    (references.hashCode) +
    (revision.hashCode) +
    (resolvedFrom.hashCode);

  @override
  String toString() => 'Note[ref=$ref, title=$title, page=$page, createdAt=$createdAt, updatedAt=$updatedAt, body=$body, html=$html, references=$references, revision=$revision, resolvedFrom=$resolvedFrom]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'ref'] = this.ref;
      json[r'title'] = this.title;
      json[r'page'] = this.page;
      json[r'created_at'] = this.createdAt.toUtc().toIso8601String();
      json[r'updated_at'] = this.updatedAt.toUtc().toIso8601String();
      json[r'body'] = this.body;
      json[r'html'] = this.html;
      json[r'references'] = this.references;
      json[r'revision'] = this.revision;
      json[r'resolved_from'] = this.resolvedFrom;
    return json;
  }

  /// Returns a new [Note] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Note? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'ref'), 'Required key "Note[ref]" is missing from JSON.');
        assert(json[r'ref'] != null, 'Required key "Note[ref]" has a null value in JSON.');
        assert(json.containsKey(r'title'), 'Required key "Note[title]" is missing from JSON.');
        assert(json[r'title'] != null, 'Required key "Note[title]" has a null value in JSON.');
        assert(json.containsKey(r'page'), 'Required key "Note[page]" is missing from JSON.');
        assert(json[r'page'] != null, 'Required key "Note[page]" has a null value in JSON.');
        assert(json.containsKey(r'created_at'), 'Required key "Note[created_at]" is missing from JSON.');
        assert(json[r'created_at'] != null, 'Required key "Note[created_at]" has a null value in JSON.');
        assert(json.containsKey(r'updated_at'), 'Required key "Note[updated_at]" is missing from JSON.');
        assert(json[r'updated_at'] != null, 'Required key "Note[updated_at]" has a null value in JSON.');
        assert(json.containsKey(r'body'), 'Required key "Note[body]" is missing from JSON.');
        assert(json[r'body'] != null, 'Required key "Note[body]" has a null value in JSON.');
        assert(json.containsKey(r'html'), 'Required key "Note[html]" is missing from JSON.');
        assert(json[r'html'] != null, 'Required key "Note[html]" has a null value in JSON.');
        assert(json.containsKey(r'references'), 'Required key "Note[references]" is missing from JSON.');
        assert(json[r'references'] != null, 'Required key "Note[references]" has a null value in JSON.');
        assert(json.containsKey(r'revision'), 'Required key "Note[revision]" is missing from JSON.');
        assert(json[r'revision'] != null, 'Required key "Note[revision]" has a null value in JSON.');
        assert(json.containsKey(r'resolved_from'), 'Required key "Note[resolved_from]" is missing from JSON.');
        assert(json[r'resolved_from'] != null, 'Required key "Note[resolved_from]" has a null value in JSON.');
        return true;
      }());

      return Note(
        ref: mapValueOfType<String>(json, r'ref')!,
        title: mapValueOfType<String>(json, r'title')!,
        page: mapValueOfType<String>(json, r'page')!,
        createdAt: mapDateTime(json, r'created_at', r'')!,
        updatedAt: mapDateTime(json, r'updated_at', r'')!,
        body: mapValueOfType<String>(json, r'body')!,
        html: mapValueOfType<String>(json, r'html')!,
        references: ReferenceDescriptor.listFromJson(json[r'references']),
        revision: RevisionMeta.fromJson(json[r'revision'])!,
        resolvedFrom: DiscussionSummary.listFromJson(json[r'resolved_from']),
      );
    }
    return null;
  }

  static List<Note> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Note>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Note.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Note> mapFromJson(dynamic json) {
    final map = <String, Note>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Note.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Note-objects as value to a dart map
  static Map<String, List<Note>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Note>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Note.listFromJson(entry.value, growable: growable,);
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
    'body',
    'html',
    'references',
    'revision',
    'resolved_from',
  };
}

