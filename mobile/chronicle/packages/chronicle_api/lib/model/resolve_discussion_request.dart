//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ResolveDiscussionRequest {
  /// Returns a new [ResolveDiscussionRequest] instance.
  ResolveDiscussionRequest({
    required this.into,
    this.page,
    this.noteRef,
    this.title,
    this.body,
  });

  /// Required, no default — resolving without a note is a deliberate choice.
  ResolveDiscussionRequestIntoEnum into;

  /// For `new_note`, the page path to file the note on. Redirects are not followed.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? page;

  /// For `existing_note`, the note to append to — `CHR-0311`.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? noteRef;

  /// For `new_note`, required. For `existing_note`, optional — omitted keeps the note's title.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? title;

  /// For `new_note` and `existing_note`, the text the thread concluded in. Markdown.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? body;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ResolveDiscussionRequest &&
    other.into == into &&
    other.page == page &&
    other.noteRef == noteRef &&
    other.title == title &&
    other.body == body;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (into.hashCode) +
    (page == null ? 0 : page!.hashCode) +
    (noteRef == null ? 0 : noteRef!.hashCode) +
    (title == null ? 0 : title!.hashCode) +
    (body == null ? 0 : body!.hashCode);

  @override
  String toString() => 'ResolveDiscussionRequest[into=$into, page=$page, noteRef=$noteRef, title=$title, body=$body]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'into'] = this.into;
    if (this.page != null) {
      json[r'page'] = this.page;
    } else {
      json[r'page'] = null;
    }
    if (this.noteRef != null) {
      json[r'note_ref'] = this.noteRef;
    } else {
      json[r'note_ref'] = null;
    }
    if (this.title != null) {
      json[r'title'] = this.title;
    } else {
      json[r'title'] = null;
    }
    if (this.body != null) {
      json[r'body'] = this.body;
    } else {
      json[r'body'] = null;
    }
    return json;
  }

  /// Returns a new [ResolveDiscussionRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ResolveDiscussionRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'into'), 'Required key "ResolveDiscussionRequest[into]" is missing from JSON.');
        assert(json[r'into'] != null, 'Required key "ResolveDiscussionRequest[into]" has a null value in JSON.');
        return true;
      }());

      return ResolveDiscussionRequest(
        into: ResolveDiscussionRequestIntoEnum.fromJson(json[r'into'])!,
        page: mapValueOfType<String>(json, r'page'),
        noteRef: mapValueOfType<String>(json, r'note_ref'),
        title: mapValueOfType<String>(json, r'title'),
        body: mapValueOfType<String>(json, r'body'),
      );
    }
    return null;
  }

  static List<ResolveDiscussionRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ResolveDiscussionRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ResolveDiscussionRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ResolveDiscussionRequest> mapFromJson(dynamic json) {
    final map = <String, ResolveDiscussionRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ResolveDiscussionRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ResolveDiscussionRequest-objects as value to a dart map
  static Map<String, List<ResolveDiscussionRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ResolveDiscussionRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ResolveDiscussionRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'into',
  };
}

/// Required, no default — resolving without a note is a deliberate choice.
class ResolveDiscussionRequestIntoEnum {
  /// Instantiate a new enum with the provided [value].
  const ResolveDiscussionRequestIntoEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const newNote = ResolveDiscussionRequestIntoEnum._(r'new_note');
  static const existingNote = ResolveDiscussionRequestIntoEnum._(r'existing_note');
  static const nothing = ResolveDiscussionRequestIntoEnum._(r'nothing');

  /// List of all possible values in this [enum][ResolveDiscussionRequestIntoEnum].
  static const values = <ResolveDiscussionRequestIntoEnum>[
    newNote,
    existingNote,
    nothing,
  ];

  static ResolveDiscussionRequestIntoEnum? fromJson(dynamic value) => ResolveDiscussionRequestIntoEnumTypeTransformer().decode(value);

  static List<ResolveDiscussionRequestIntoEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ResolveDiscussionRequestIntoEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ResolveDiscussionRequestIntoEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [ResolveDiscussionRequestIntoEnum] to String,
/// and [decode] dynamic data back to [ResolveDiscussionRequestIntoEnum].
class ResolveDiscussionRequestIntoEnumTypeTransformer {
  factory ResolveDiscussionRequestIntoEnumTypeTransformer() => _instance ??= const ResolveDiscussionRequestIntoEnumTypeTransformer._();

  const ResolveDiscussionRequestIntoEnumTypeTransformer._();

  String encode(ResolveDiscussionRequestIntoEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a ResolveDiscussionRequestIntoEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  ResolveDiscussionRequestIntoEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'new_note': return ResolveDiscussionRequestIntoEnum.newNote;
        case r'existing_note': return ResolveDiscussionRequestIntoEnum.existingNote;
        case r'nothing': return ResolveDiscussionRequestIntoEnum.nothing;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [ResolveDiscussionRequestIntoEnumTypeTransformer] instance.
  static ResolveDiscussionRequestIntoEnumTypeTransformer? _instance;
}


