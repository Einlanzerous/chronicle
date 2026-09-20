//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TriageResult {
  /// Returns a new [TriageResult] instance.
  TriageResult({
    required this.memoId,
    required this.status,
    this.destination,
    this.ticketKey,
    this.ticketUrl,
    this.noteRef,
    this.discussionRef,
    this.generation,
    this.cleared = const [],
    this.reason,
  });

  String memoId;

  /// What happened to this one — landed, refused, stale, and so on.
  String status;

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
  String? ticketKey;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? ticketUrl;

  /// `CHR-0311`, when this decision produced a note.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? noteRef;

  /// `DSC-0007`, when it produced a discussion.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? discussionRef;

  int? generation;

  List<ClearedField> cleared;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? reason;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TriageResult &&
    other.memoId == memoId &&
    other.status == status &&
    other.destination == destination &&
    other.ticketKey == ticketKey &&
    other.ticketUrl == ticketUrl &&
    other.noteRef == noteRef &&
    other.discussionRef == discussionRef &&
    other.generation == generation &&
    _deepEquality.equals(other.cleared, cleared) &&
    other.reason == reason;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode) +
    (status.hashCode) +
    (destination == null ? 0 : destination!.hashCode) +
    (ticketKey == null ? 0 : ticketKey!.hashCode) +
    (ticketUrl == null ? 0 : ticketUrl!.hashCode) +
    (noteRef == null ? 0 : noteRef!.hashCode) +
    (discussionRef == null ? 0 : discussionRef!.hashCode) +
    (generation == null ? 0 : generation!.hashCode) +
    (cleared.hashCode) +
    (reason == null ? 0 : reason!.hashCode);

  @override
  String toString() => 'TriageResult[memoId=$memoId, status=$status, destination=$destination, ticketKey=$ticketKey, ticketUrl=$ticketUrl, noteRef=$noteRef, discussionRef=$discussionRef, generation=$generation, cleared=$cleared, reason=$reason]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
      json[r'status'] = this.status;
    if (this.destination != null) {
      json[r'destination'] = this.destination;
    } else {
      json[r'destination'] = null;
    }
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
    if (this.generation != null) {
      json[r'generation'] = this.generation;
    } else {
      json[r'generation'] = null;
    }
      json[r'cleared'] = this.cleared;
    if (this.reason != null) {
      json[r'reason'] = this.reason;
    } else {
      json[r'reason'] = null;
    }
    return json;
  }

  /// Returns a new [TriageResult] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TriageResult? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "TriageResult[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "TriageResult[memo_id]" has a null value in JSON.');
        assert(json.containsKey(r'status'), 'Required key "TriageResult[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "TriageResult[status]" has a null value in JSON.');
        return true;
      }());

      return TriageResult(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        status: mapValueOfType<String>(json, r'status')!,
        destination: mapValueOfType<String>(json, r'destination'),
        ticketKey: mapValueOfType<String>(json, r'ticket_key'),
        ticketUrl: mapValueOfType<String>(json, r'ticket_url'),
        noteRef: mapValueOfType<String>(json, r'note_ref'),
        discussionRef: mapValueOfType<String>(json, r'discussion_ref'),
        generation: mapValueOfType<int>(json, r'generation'),
        cleared: ClearedField.listFromJson(json[r'cleared']),
        reason: mapValueOfType<String>(json, r'reason'),
      );
    }
    return null;
  }

  static List<TriageResult> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TriageResult>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TriageResult.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TriageResult> mapFromJson(dynamic json) {
    final map = <String, TriageResult>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TriageResult.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TriageResult-objects as value to a dart map
  static Map<String, List<TriageResult>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TriageResult>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TriageResult.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
    'status',
  };
}

