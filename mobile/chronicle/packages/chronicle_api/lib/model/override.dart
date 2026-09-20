//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Override {
  /// Returns a new [Override] instance.
  Override({
    this.destination,
    this.title,
    this.projectKey,
    this.ticketType,
    this.description,
    this.verb,
    this.targetNote,
    this.pagePath,
    this.body,
    this.openingPost,
  });

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? destination;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? title;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? projectKey;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? ticketType;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? description;

  OverrideVerbEnum? verb;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? targetNote;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? pagePath;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? body;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? openingPost;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Override &&
    other.destination == destination &&
    other.title == title &&
    other.projectKey == projectKey &&
    other.ticketType == ticketType &&
    other.description == description &&
    other.verb == verb &&
    other.targetNote == targetNote &&
    other.pagePath == pagePath &&
    other.body == body &&
    other.openingPost == openingPost;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (destination == null ? 0 : destination!.hashCode) +
    (title == null ? 0 : title!.hashCode) +
    (projectKey == null ? 0 : projectKey!.hashCode) +
    (ticketType == null ? 0 : ticketType!.hashCode) +
    (description == null ? 0 : description!.hashCode) +
    (verb == null ? 0 : verb!.hashCode) +
    (targetNote == null ? 0 : targetNote!.hashCode) +
    (pagePath == null ? 0 : pagePath!.hashCode) +
    (body == null ? 0 : body!.hashCode) +
    (openingPost == null ? 0 : openingPost!.hashCode);

  @override
  String toString() => 'Override[destination=$destination, title=$title, projectKey=$projectKey, ticketType=$ticketType, description=$description, verb=$verb, targetNote=$targetNote, pagePath=$pagePath, body=$body, openingPost=$openingPost]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.destination != null) {
      json[r'destination'] = this.destination;
    } else {
      json[r'destination'] = null;
    }
    if (this.title != null) {
      json[r'title'] = this.title;
    } else {
      json[r'title'] = null;
    }
    if (this.projectKey != null) {
      json[r'project_key'] = this.projectKey;
    } else {
      json[r'project_key'] = null;
    }
    if (this.ticketType != null) {
      json[r'ticket_type'] = this.ticketType;
    } else {
      json[r'ticket_type'] = null;
    }
    if (this.description != null) {
      json[r'description'] = this.description;
    } else {
      json[r'description'] = null;
    }
    if (this.verb != null) {
      json[r'verb'] = this.verb;
    } else {
      json[r'verb'] = null;
    }
    if (this.targetNote != null) {
      json[r'target_note'] = this.targetNote;
    } else {
      json[r'target_note'] = null;
    }
    if (this.pagePath != null) {
      json[r'page_path'] = this.pagePath;
    } else {
      json[r'page_path'] = null;
    }
    if (this.body != null) {
      json[r'body'] = this.body;
    } else {
      json[r'body'] = null;
    }
    if (this.openingPost != null) {
      json[r'opening_post'] = this.openingPost;
    } else {
      json[r'opening_post'] = null;
    }
    return json;
  }

  /// Returns a new [Override] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Override? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return Override(
        destination: mapValueOfType<String>(json, r'destination'),
        title: mapValueOfType<String>(json, r'title'),
        projectKey: mapValueOfType<String>(json, r'project_key'),
        ticketType: mapValueOfType<String>(json, r'ticket_type'),
        description: mapValueOfType<String>(json, r'description'),
        verb: OverrideVerbEnum.fromJson(json[r'verb']),
        targetNote: mapValueOfType<String>(json, r'target_note'),
        pagePath: mapValueOfType<String>(json, r'page_path'),
        body: mapValueOfType<String>(json, r'body'),
        openingPost: mapValueOfType<String>(json, r'opening_post'),
      );
    }
    return null;
  }

  static List<Override> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Override>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Override.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Override> mapFromJson(dynamic json) {
    final map = <String, Override>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Override.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Override-objects as value to a dart map
  static Map<String, List<Override>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Override>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Override.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}


class OverrideVerbEnum {
  /// Instantiate a new enum with the provided [value].
  const OverrideVerbEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const create = OverrideVerbEnum._(r'create');
  static const append = OverrideVerbEnum._(r'append');
  static const supersede = OverrideVerbEnum._(r'supersede');
  static const relate = OverrideVerbEnum._(r'relate');

  /// List of all possible values in this [enum][OverrideVerbEnum].
  static const values = <OverrideVerbEnum>[
    create,
    append,
    supersede,
    relate,
  ];

  static OverrideVerbEnum? fromJson(dynamic value) => OverrideVerbEnumTypeTransformer().decode(value);

  static List<OverrideVerbEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <OverrideVerbEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = OverrideVerbEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [OverrideVerbEnum] to String,
/// and [decode] dynamic data back to [OverrideVerbEnum].
class OverrideVerbEnumTypeTransformer {
  factory OverrideVerbEnumTypeTransformer() => _instance ??= const OverrideVerbEnumTypeTransformer._();

  const OverrideVerbEnumTypeTransformer._();

  String encode(OverrideVerbEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a OverrideVerbEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  OverrideVerbEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'create': return OverrideVerbEnum.create;
        case r'append': return OverrideVerbEnum.append;
        case r'supersede': return OverrideVerbEnum.supersede;
        case r'relate': return OverrideVerbEnum.relate;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [OverrideVerbEnumTypeTransformer] instance.
  static OverrideVerbEnumTypeTransformer? _instance;
}


