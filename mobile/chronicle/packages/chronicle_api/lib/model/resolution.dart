//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Resolution {
  /// Returns a new [Resolution] instance.
  Resolution({
    required this.token,
    required this.state,
    this.upstream,
    this.fetchedAt,
    this.lastResolvedAt,
    this.explain,
  });

  /// The descriptor's `token`, echoed so a client aligns the answer without depending on order.
  String token;

  /// Five, not two, and the length is the point.  - `resolved` — the upstream answered and `upstream` is its answer. - `broken` — the upstream answered that there is no such thing, or   that the reference is wrong. Still a fact about the referent, so   `upstream` is present with whatever the answer contained. - `unreachable` — Chronicle ASKED and could not obtain an answer. A   fact about Chronicle's knowledge, and the one no upstream can   report. `last_resolved_at` says how long the outage has run. - `unconfigured` — this deployment has no credential for that   upstream: nothing was dialled and nothing will be until a   redeploy. A permanent state and not an outage; a card reading   \"unreachable\" here would send somebody to check a service that   is fine. - `unchecked` — NOBODY ASKED. The cap, the budget, or an earlier   failure in this batch declined to dial, and no claim about this   reference is being made. Draw it as \"not checked\", never as   \"checking…\" — that is the state that is never answered. 
  ResolutionStateEnum state;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  ResolutionUpstream? upstream;

  /// When the attempt was made. Present on `resolved`, `broken` and `unreachable`; **absent on `unchecked` and `unconfigured`**, where nothing was attempted (CHRN-97 ruling 7). Serialised as required it would carry `0001-01-01T00:00:00Z` on exactly the two states that exist to stop Chronicle making claims, and an ordinary relative-time formatter renders that as \"checked 2,025 years ago\". The Go type is a non-pointer that is zero there by convention; a convention does not cross a language boundary, so the wire makes the illegal state unrepresentable instead. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? fetchedAt;

  /// When this reference last resolved SUCCESSFULLY, within this process's memory. Absence means *never resolved within this process's memory* — not *not resolved now*. It travels through an `unreachable` so a card can say how long an outage has run; a client that read absence as staleness would render a cold start as an outage. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? lastResolvedAt;

  /// One sentence — the upstream's own where it has one (Amber's is relayed verbatim), Chronicle's for the states no upstream can report. Absent when a resolved answer had nothing to add. **It never carries a relative time**: \"4 s ago\" is true at serialisation and false in the hands of a client holding the payload. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? explain;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Resolution &&
    other.token == token &&
    other.state == state &&
    other.upstream == upstream &&
    other.fetchedAt == fetchedAt &&
    other.lastResolvedAt == lastResolvedAt &&
    other.explain == explain;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (token.hashCode) +
    (state.hashCode) +
    (upstream == null ? 0 : upstream!.hashCode) +
    (fetchedAt == null ? 0 : fetchedAt!.hashCode) +
    (lastResolvedAt == null ? 0 : lastResolvedAt!.hashCode) +
    (explain == null ? 0 : explain!.hashCode);

  @override
  String toString() => 'Resolution[token=$token, state=$state, upstream=$upstream, fetchedAt=$fetchedAt, lastResolvedAt=$lastResolvedAt, explain=$explain]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'token'] = this.token;
      json[r'state'] = this.state;
    if (this.upstream != null) {
      json[r'upstream'] = this.upstream;
    } else {
      json[r'upstream'] = null;
    }
    if (this.fetchedAt != null) {
      json[r'fetched_at'] = this.fetchedAt!.toUtc().toIso8601String();
    } else {
      json[r'fetched_at'] = null;
    }
    if (this.lastResolvedAt != null) {
      json[r'last_resolved_at'] = this.lastResolvedAt!.toUtc().toIso8601String();
    } else {
      json[r'last_resolved_at'] = null;
    }
    if (this.explain != null) {
      json[r'explain'] = this.explain;
    } else {
      json[r'explain'] = null;
    }
    return json;
  }

  /// Returns a new [Resolution] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Resolution? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'token'), 'Required key "Resolution[token]" is missing from JSON.');
        assert(json[r'token'] != null, 'Required key "Resolution[token]" has a null value in JSON.');
        assert(json.containsKey(r'state'), 'Required key "Resolution[state]" is missing from JSON.');
        assert(json[r'state'] != null, 'Required key "Resolution[state]" has a null value in JSON.');
        return true;
      }());

      return Resolution(
        token: mapValueOfType<String>(json, r'token')!,
        state: ResolutionStateEnum.fromJson(json[r'state'])!,
        upstream: ResolutionUpstream.fromJson(json[r'upstream']),
        fetchedAt: mapDateTime(json, r'fetched_at', r''),
        lastResolvedAt: mapDateTime(json, r'last_resolved_at', r''),
        explain: mapValueOfType<String>(json, r'explain'),
      );
    }
    return null;
  }

  static List<Resolution> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Resolution>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Resolution.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Resolution> mapFromJson(dynamic json) {
    final map = <String, Resolution>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Resolution.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Resolution-objects as value to a dart map
  static Map<String, List<Resolution>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Resolution>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Resolution.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'token',
    'state',
  };
}

/// Five, not two, and the length is the point.  - `resolved` — the upstream answered and `upstream` is its answer. - `broken` — the upstream answered that there is no such thing, or   that the reference is wrong. Still a fact about the referent, so   `upstream` is present with whatever the answer contained. - `unreachable` — Chronicle ASKED and could not obtain an answer. A   fact about Chronicle's knowledge, and the one no upstream can   report. `last_resolved_at` says how long the outage has run. - `unconfigured` — this deployment has no credential for that   upstream: nothing was dialled and nothing will be until a   redeploy. A permanent state and not an outage; a card reading   \"unreachable\" here would send somebody to check a service that   is fine. - `unchecked` — NOBODY ASKED. The cap, the budget, or an earlier   failure in this batch declined to dial, and no claim about this   reference is being made. Draw it as \"not checked\", never as   \"checking…\" — that is the state that is never answered. 
class ResolutionStateEnum {
  /// Instantiate a new enum with the provided [value].
  const ResolutionStateEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const resolved = ResolutionStateEnum._(r'resolved');
  static const broken = ResolutionStateEnum._(r'broken');
  static const unreachable = ResolutionStateEnum._(r'unreachable');
  static const unconfigured = ResolutionStateEnum._(r'unconfigured');
  static const unchecked = ResolutionStateEnum._(r'unchecked');

  /// List of all possible values in this [enum][ResolutionStateEnum].
  static const values = <ResolutionStateEnum>[
    resolved,
    broken,
    unreachable,
    unconfigured,
    unchecked,
  ];

  static ResolutionStateEnum? fromJson(dynamic value) => ResolutionStateEnumTypeTransformer().decode(value);

  static List<ResolutionStateEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ResolutionStateEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ResolutionStateEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [ResolutionStateEnum] to String,
/// and [decode] dynamic data back to [ResolutionStateEnum].
class ResolutionStateEnumTypeTransformer {
  factory ResolutionStateEnumTypeTransformer() => _instance ??= const ResolutionStateEnumTypeTransformer._();

  const ResolutionStateEnumTypeTransformer._();

  String encode(ResolutionStateEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a ResolutionStateEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  ResolutionStateEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'resolved': return ResolutionStateEnum.resolved;
        case r'broken': return ResolutionStateEnum.broken;
        case r'unreachable': return ResolutionStateEnum.unreachable;
        case r'unconfigured': return ResolutionStateEnum.unconfigured;
        case r'unchecked': return ResolutionStateEnum.unchecked;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [ResolutionStateEnumTypeTransformer] instance.
  static ResolutionStateEnumTypeTransformer? _instance;
}


