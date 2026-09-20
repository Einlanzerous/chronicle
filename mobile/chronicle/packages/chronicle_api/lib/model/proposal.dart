//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Proposal {
  /// Returns a new [Proposal] instance.
  Proposal({
    required this.generated,
    required this.destination,
    required this.confidence,
    required this.reason,
    this.title,
    required this.nearestPage,
    this.projectKey,
    this.ticketType,
    this.description,
    this.pagePath,
    this.verb,
    this.targetNote,
    this.body,
    this.openingPost,
  });

  Generated generated;

  ProposalDestinationEnum destination;

  double confidence;

  /// The model's own sentence, for a person deciding whether to agree.
  String reason;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? title;

  /// The closest existing page, when the proposal is a note. Always present as a field so \"no nearby page\" is `null` rather than absent, which a client would otherwise read as \"not computed\". 
  String? nearestPage;

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

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? pagePath;

  /// What confirming does to existing authored text. `create` makes a new note; the other three touch one that exists, which is why `note_revisions_guard` requires a confirming person who is never an agent. 
  ProposalVerbEnum? verb;

  /// The note the verb acts on — `CHR-0311`. **Required unless the verb is `create`**, and the reason it is on the card rather than only in the accept call: a person confirming an `append` is agreeing to change a specific piece of authored text, and cannot agree to that without being told which. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? targetNote;

  /// The drafted note text, for a `NOTE` proposal. What acceptance writes as the first revision — so a person confirming without seeing it would be confirming text they have not read, which is the one thing the confirmation exists to prevent. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? body;

  /// The drafted first turn, for a `DISCUSSION` proposal. Same reasoning as `body`.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? openingPost;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Proposal &&
    other.generated == generated &&
    other.destination == destination &&
    other.confidence == confidence &&
    other.reason == reason &&
    other.title == title &&
    other.nearestPage == nearestPage &&
    other.projectKey == projectKey &&
    other.ticketType == ticketType &&
    other.description == description &&
    other.pagePath == pagePath &&
    other.verb == verb &&
    other.targetNote == targetNote &&
    other.body == body &&
    other.openingPost == openingPost;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (generated.hashCode) +
    (destination.hashCode) +
    (confidence.hashCode) +
    (reason.hashCode) +
    (title == null ? 0 : title!.hashCode) +
    (nearestPage == null ? 0 : nearestPage!.hashCode) +
    (projectKey == null ? 0 : projectKey!.hashCode) +
    (ticketType == null ? 0 : ticketType!.hashCode) +
    (description == null ? 0 : description!.hashCode) +
    (pagePath == null ? 0 : pagePath!.hashCode) +
    (verb == null ? 0 : verb!.hashCode) +
    (targetNote == null ? 0 : targetNote!.hashCode) +
    (body == null ? 0 : body!.hashCode) +
    (openingPost == null ? 0 : openingPost!.hashCode);

  @override
  String toString() => 'Proposal[generated=$generated, destination=$destination, confidence=$confidence, reason=$reason, title=$title, nearestPage=$nearestPage, projectKey=$projectKey, ticketType=$ticketType, description=$description, pagePath=$pagePath, verb=$verb, targetNote=$targetNote, body=$body, openingPost=$openingPost]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'generated'] = this.generated;
      json[r'destination'] = this.destination;
      json[r'confidence'] = this.confidence;
      json[r'reason'] = this.reason;
    if (this.title != null) {
      json[r'title'] = this.title;
    } else {
      json[r'title'] = null;
    }
    if (this.nearestPage != null) {
      json[r'nearest_page'] = this.nearestPage;
    } else {
      json[r'nearest_page'] = null;
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
    if (this.pagePath != null) {
      json[r'page_path'] = this.pagePath;
    } else {
      json[r'page_path'] = null;
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

  /// Returns a new [Proposal] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Proposal? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'generated'), 'Required key "Proposal[generated]" is missing from JSON.');
        assert(json[r'generated'] != null, 'Required key "Proposal[generated]" has a null value in JSON.');
        assert(json.containsKey(r'destination'), 'Required key "Proposal[destination]" is missing from JSON.');
        assert(json[r'destination'] != null, 'Required key "Proposal[destination]" has a null value in JSON.');
        assert(json.containsKey(r'confidence'), 'Required key "Proposal[confidence]" is missing from JSON.');
        assert(json[r'confidence'] != null, 'Required key "Proposal[confidence]" has a null value in JSON.');
        assert(json.containsKey(r'reason'), 'Required key "Proposal[reason]" is missing from JSON.');
        assert(json[r'reason'] != null, 'Required key "Proposal[reason]" has a null value in JSON.');
        assert(json.containsKey(r'nearest_page'), 'Required key "Proposal[nearest_page]" is missing from JSON.');
        return true;
      }());

      return Proposal(
        generated: Generated.fromJson(json[r'generated'])!,
        destination: ProposalDestinationEnum.fromJson(json[r'destination'])!,
        confidence: mapValueOfType<double>(json, r'confidence')!,
        reason: mapValueOfType<String>(json, r'reason')!,
        title: mapValueOfType<String>(json, r'title'),
        nearestPage: mapValueOfType<String>(json, r'nearest_page'),
        projectKey: mapValueOfType<String>(json, r'project_key'),
        ticketType: mapValueOfType<String>(json, r'ticket_type'),
        description: mapValueOfType<String>(json, r'description'),
        pagePath: mapValueOfType<String>(json, r'page_path'),
        verb: ProposalVerbEnum.fromJson(json[r'verb']),
        targetNote: mapValueOfType<String>(json, r'target_note'),
        body: mapValueOfType<String>(json, r'body'),
        openingPost: mapValueOfType<String>(json, r'opening_post'),
      );
    }
    return null;
  }

  static List<Proposal> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Proposal>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Proposal.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Proposal> mapFromJson(dynamic json) {
    final map = <String, Proposal>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Proposal.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Proposal-objects as value to a dart map
  static Map<String, List<Proposal>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Proposal>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Proposal.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'generated',
    'destination',
    'confidence',
    'reason',
    'nearest_page',
  };
}


class ProposalDestinationEnum {
  /// Instantiate a new enum with the provided [value].
  const ProposalDestinationEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const NOTE = ProposalDestinationEnum._(r'NOTE');
  static const TICKET = ProposalDestinationEnum._(r'TICKET');
  static const DISCUSSION = ProposalDestinationEnum._(r'DISCUSSION');
  static const DISCARD = ProposalDestinationEnum._(r'DISCARD');

  /// List of all possible values in this [enum][ProposalDestinationEnum].
  static const values = <ProposalDestinationEnum>[
    NOTE,
    TICKET,
    DISCUSSION,
    DISCARD,
  ];

  static ProposalDestinationEnum? fromJson(dynamic value) => ProposalDestinationEnumTypeTransformer().decode(value);

  static List<ProposalDestinationEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ProposalDestinationEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ProposalDestinationEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [ProposalDestinationEnum] to String,
/// and [decode] dynamic data back to [ProposalDestinationEnum].
class ProposalDestinationEnumTypeTransformer {
  factory ProposalDestinationEnumTypeTransformer() => _instance ??= const ProposalDestinationEnumTypeTransformer._();

  const ProposalDestinationEnumTypeTransformer._();

  String encode(ProposalDestinationEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a ProposalDestinationEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  ProposalDestinationEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'NOTE': return ProposalDestinationEnum.NOTE;
        case r'TICKET': return ProposalDestinationEnum.TICKET;
        case r'DISCUSSION': return ProposalDestinationEnum.DISCUSSION;
        case r'DISCARD': return ProposalDestinationEnum.DISCARD;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [ProposalDestinationEnumTypeTransformer] instance.
  static ProposalDestinationEnumTypeTransformer? _instance;
}


/// What confirming does to existing authored text. `create` makes a new note; the other three touch one that exists, which is why `note_revisions_guard` requires a confirming person who is never an agent. 
class ProposalVerbEnum {
  /// Instantiate a new enum with the provided [value].
  const ProposalVerbEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const create = ProposalVerbEnum._(r'create');
  static const append = ProposalVerbEnum._(r'append');
  static const supersede = ProposalVerbEnum._(r'supersede');
  static const relate = ProposalVerbEnum._(r'relate');

  /// List of all possible values in this [enum][ProposalVerbEnum].
  static const values = <ProposalVerbEnum>[
    create,
    append,
    supersede,
    relate,
  ];

  static ProposalVerbEnum? fromJson(dynamic value) => ProposalVerbEnumTypeTransformer().decode(value);

  static List<ProposalVerbEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ProposalVerbEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ProposalVerbEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [ProposalVerbEnum] to String,
/// and [decode] dynamic data back to [ProposalVerbEnum].
class ProposalVerbEnumTypeTransformer {
  factory ProposalVerbEnumTypeTransformer() => _instance ??= const ProposalVerbEnumTypeTransformer._();

  const ProposalVerbEnumTypeTransformer._();

  String encode(ProposalVerbEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a ProposalVerbEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  ProposalVerbEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'create': return ProposalVerbEnum.create;
        case r'append': return ProposalVerbEnum.append;
        case r'supersede': return ProposalVerbEnum.supersede;
        case r'relate': return ProposalVerbEnum.relate;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [ProposalVerbEnumTypeTransformer] instance.
  static ProposalVerbEnumTypeTransformer? _instance;
}


