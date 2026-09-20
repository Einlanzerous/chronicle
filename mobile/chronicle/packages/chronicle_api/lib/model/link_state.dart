//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class LinkState {
  /// Returns a new [LinkState] instance.
  LinkState({
    required this.destination,
    required this.state,
    required this.decidedAt,
    this.ticketKey,
    this.ticketUrl,
    this.noteRef,
    this.discussionRef,
    this.candidateKeys = const [],
    this.sweptAt,
    this.refusedStatus,
    this.refusedReason,
    this.refusedAt,
  });

  String destination;

  /// `in_flight`, `linked`, `unresolved`, `ambiguous` or `refused`.
  String state;

  DateTime decidedAt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? ticketKey;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? ticketUrl;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? noteRef;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? discussionRef;

  /// What an `ambiguous` sweep found. More than one ticket carries this memo's id, and the service will not pick — choosing for a person here is how the wrong ticket gets linked silently. 
  List<String> candidateKeys;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? sweptAt;

  /// The status Switchyard answered. Kept because it CACHES 4xx: an identical resend gets the same refusal, so the remedy is a new idempotency key rather than a retry. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? refusedStatus;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? refusedReason;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? refusedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is LinkState &&
    other.destination == destination &&
    other.state == state &&
    other.decidedAt == decidedAt &&
    other.ticketKey == ticketKey &&
    other.ticketUrl == ticketUrl &&
    other.noteRef == noteRef &&
    other.discussionRef == discussionRef &&
    _deepEquality.equals(other.candidateKeys, candidateKeys) &&
    other.sweptAt == sweptAt &&
    other.refusedStatus == refusedStatus &&
    other.refusedReason == refusedReason &&
    other.refusedAt == refusedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (destination.hashCode) +
    (state.hashCode) +
    (decidedAt.hashCode) +
    (ticketKey == null ? 0 : ticketKey!.hashCode) +
    (ticketUrl == null ? 0 : ticketUrl!.hashCode) +
    (noteRef == null ? 0 : noteRef!.hashCode) +
    (discussionRef == null ? 0 : discussionRef!.hashCode) +
    (candidateKeys.hashCode) +
    (sweptAt == null ? 0 : sweptAt!.hashCode) +
    (refusedStatus == null ? 0 : refusedStatus!.hashCode) +
    (refusedReason == null ? 0 : refusedReason!.hashCode) +
    (refusedAt == null ? 0 : refusedAt!.hashCode);

  @override
  String toString() => 'LinkState[destination=$destination, state=$state, decidedAt=$decidedAt, ticketKey=$ticketKey, ticketUrl=$ticketUrl, noteRef=$noteRef, discussionRef=$discussionRef, candidateKeys=$candidateKeys, sweptAt=$sweptAt, refusedStatus=$refusedStatus, refusedReason=$refusedReason, refusedAt=$refusedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'destination'] = this.destination;
      json[r'state'] = this.state;
      json[r'decided_at'] = this.decidedAt.toUtc().toIso8601String();
    if (this.ticketKey != null) {
      json[r'ticket_key'] = this.ticketKey;
    } else {
      json[r'ticket_key'] = null;
    }
    if (this.ticketUrl != null) {
      json[r'ticket_url'] = this.ticketUrl;
    } else {
      json[r'ticket_url'] = null;
    }
    if (this.noteRef != null) {
      json[r'note_ref'] = this.noteRef;
    } else {
      json[r'note_ref'] = null;
    }
    if (this.discussionRef != null) {
      json[r'discussion_ref'] = this.discussionRef;
    } else {
      json[r'discussion_ref'] = null;
    }
      json[r'candidate_keys'] = this.candidateKeys;
    if (this.sweptAt != null) {
      json[r'swept_at'] = this.sweptAt!.toUtc().toIso8601String();
    } else {
      json[r'swept_at'] = null;
    }
    if (this.refusedStatus != null) {
      json[r'refused_status'] = this.refusedStatus;
    } else {
      json[r'refused_status'] = null;
    }
    if (this.refusedReason != null) {
      json[r'refused_reason'] = this.refusedReason;
    } else {
      json[r'refused_reason'] = null;
    }
    if (this.refusedAt != null) {
      json[r'refused_at'] = this.refusedAt!.toUtc().toIso8601String();
    } else {
      json[r'refused_at'] = null;
    }
    return json;
  }

  /// Returns a new [LinkState] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static LinkState? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'destination'), 'Required key "LinkState[destination]" is missing from JSON.');
        assert(json[r'destination'] != null, 'Required key "LinkState[destination]" has a null value in JSON.');
        assert(json.containsKey(r'state'), 'Required key "LinkState[state]" is missing from JSON.');
        assert(json[r'state'] != null, 'Required key "LinkState[state]" has a null value in JSON.');
        assert(json.containsKey(r'decided_at'), 'Required key "LinkState[decided_at]" is missing from JSON.');
        assert(json[r'decided_at'] != null, 'Required key "LinkState[decided_at]" has a null value in JSON.');
        return true;
      }());

      return LinkState(
        destination: mapValueOfType<String>(json, r'destination')!,
        state: mapValueOfType<String>(json, r'state')!,
        decidedAt: mapDateTime(json, r'decided_at', r'')!,
        ticketKey: mapValueOfType<String>(json, r'ticket_key'),
        ticketUrl: mapValueOfType<String>(json, r'ticket_url'),
        noteRef: mapValueOfType<String>(json, r'note_ref'),
        discussionRef: mapValueOfType<String>(json, r'discussion_ref'),
        candidateKeys: json[r'candidate_keys'] is Iterable
            ? (json[r'candidate_keys'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        sweptAt: mapDateTime(json, r'swept_at', r''),
        refusedStatus: mapValueOfType<int>(json, r'refused_status'),
        refusedReason: mapValueOfType<String>(json, r'refused_reason'),
        refusedAt: mapDateTime(json, r'refused_at', r''),
      );
    }
    return null;
  }

  static List<LinkState> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <LinkState>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = LinkState.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, LinkState> mapFromJson(dynamic json) {
    final map = <String, LinkState>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = LinkState.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of LinkState-objects as value to a dart map
  static Map<String, List<LinkState>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<LinkState>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = LinkState.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'destination',
    'state',
    'decided_at',
  };
}

