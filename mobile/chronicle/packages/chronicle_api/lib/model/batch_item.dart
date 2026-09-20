//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class BatchItem {
  /// Returns a new [BatchItem] instance.
  BatchItem({
    required this.memoId,
    required this.capturedAt,
    this.durationMs,
    required this.excerpt,
    required this.proposer,
    required this.generation,
    required this.status,
    this.proposal,
    this.clearedFields = const [],
    this.error,
    required this.preAcceptable,
    this.link,
  });

  String memoId;

  DateTime capturedAt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? durationMs;

  /// The transcript, bounded. **The evidence for the proposal**, and the reason this is not a notification with a verb on it: a person confirming a routing decision is confirming it against what was said, not against a label. 
  String excerpt;

  /// Which model proposed, runner-qualified.
  String proposer;

  /// Which generation of the proposal this is. A decision carries it back so that a proposal regenerated since the client read it is REFUSED rather than confirmed against text nobody saw. Null when there is no proposal to be a generation of. 
  int? generation;

  /// Where this memo is in routing — `proposed`, `needs_input`, `pre_acceptable`, and the error states. Not the memo's own state. 
  String status;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Proposal? proposal;

  /// Fields the Scribe proposed and the validator REMOVED, each with why. Reported rather than dropped: a silently-cleared `project_key` is a ticket filed against nothing, and the person confirming is the only one who can tell whether the clearing was right. 
  List<ClearedField> clearedFields;

  /// Why this memo has no usable proposal, when it has none.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? error;

  /// Whether this proposal clears CHRN-36's confidence threshold — the one a run of the eval set chose. It is a hint for the UI's default, never a licence to skip the confirmation. 
  bool preAcceptable;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  LinkState? link;

  @override
  bool operator ==(Object other) => identical(this, other) || other is BatchItem &&
    other.memoId == memoId &&
    other.capturedAt == capturedAt &&
    other.durationMs == durationMs &&
    other.excerpt == excerpt &&
    other.proposer == proposer &&
    other.generation == generation &&
    other.status == status &&
    other.proposal == proposal &&
    _deepEquality.equals(other.clearedFields, clearedFields) &&
    other.error == error &&
    other.preAcceptable == preAcceptable &&
    other.link == link;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode) +
    (capturedAt.hashCode) +
    (durationMs == null ? 0 : durationMs!.hashCode) +
    (excerpt.hashCode) +
    (proposer.hashCode) +
    (generation == null ? 0 : generation!.hashCode) +
    (status.hashCode) +
    (proposal == null ? 0 : proposal!.hashCode) +
    (clearedFields.hashCode) +
    (error == null ? 0 : error!.hashCode) +
    (preAcceptable.hashCode) +
    (link == null ? 0 : link!.hashCode);

  @override
  String toString() => 'BatchItem[memoId=$memoId, capturedAt=$capturedAt, durationMs=$durationMs, excerpt=$excerpt, proposer=$proposer, generation=$generation, status=$status, proposal=$proposal, clearedFields=$clearedFields, error=$error, preAcceptable=$preAcceptable, link=$link]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
      json[r'captured_at'] = this.capturedAt.toUtc().toIso8601String();
    if (this.durationMs != null) {
      json[r'duration_ms'] = this.durationMs;
    } else {
      json[r'duration_ms'] = null;
    }
      json[r'excerpt'] = this.excerpt;
      json[r'proposer'] = this.proposer;
    if (this.generation != null) {
      json[r'generation'] = this.generation;
    } else {
      json[r'generation'] = null;
    }
      json[r'status'] = this.status;
    if (this.proposal != null) {
      json[r'proposal'] = this.proposal;
    } else {
      json[r'proposal'] = null;
    }
      json[r'cleared_fields'] = this.clearedFields;
    if (this.error != null) {
      json[r'error'] = this.error;
    } else {
      json[r'error'] = null;
    }
      json[r'pre_acceptable'] = this.preAcceptable;
    if (this.link != null) {
      json[r'link'] = this.link;
    } else {
      json[r'link'] = null;
    }
    return json;
  }

  /// Returns a new [BatchItem] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static BatchItem? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "BatchItem[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "BatchItem[memo_id]" has a null value in JSON.');
        assert(json.containsKey(r'captured_at'), 'Required key "BatchItem[captured_at]" is missing from JSON.');
        assert(json[r'captured_at'] != null, 'Required key "BatchItem[captured_at]" has a null value in JSON.');
        assert(json.containsKey(r'excerpt'), 'Required key "BatchItem[excerpt]" is missing from JSON.');
        assert(json[r'excerpt'] != null, 'Required key "BatchItem[excerpt]" has a null value in JSON.');
        assert(json.containsKey(r'proposer'), 'Required key "BatchItem[proposer]" is missing from JSON.');
        assert(json[r'proposer'] != null, 'Required key "BatchItem[proposer]" has a null value in JSON.');
        assert(json.containsKey(r'generation'), 'Required key "BatchItem[generation]" is missing from JSON.');
        assert(json.containsKey(r'status'), 'Required key "BatchItem[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "BatchItem[status]" has a null value in JSON.');
        assert(json.containsKey(r'pre_acceptable'), 'Required key "BatchItem[pre_acceptable]" is missing from JSON.');
        assert(json[r'pre_acceptable'] != null, 'Required key "BatchItem[pre_acceptable]" has a null value in JSON.');
        return true;
      }());

      return BatchItem(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        capturedAt: mapDateTime(json, r'captured_at', r'')!,
        durationMs: mapValueOfType<int>(json, r'duration_ms'),
        excerpt: mapValueOfType<String>(json, r'excerpt')!,
        proposer: mapValueOfType<String>(json, r'proposer')!,
        generation: mapValueOfType<int>(json, r'generation'),
        status: mapValueOfType<String>(json, r'status')!,
        proposal: Proposal.fromJson(json[r'proposal']),
        clearedFields: ClearedField.listFromJson(json[r'cleared_fields']),
        error: mapValueOfType<String>(json, r'error'),
        preAcceptable: mapValueOfType<bool>(json, r'pre_acceptable')!,
        link: LinkState.fromJson(json[r'link']),
      );
    }
    return null;
  }

  static List<BatchItem> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <BatchItem>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = BatchItem.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, BatchItem> mapFromJson(dynamic json) {
    final map = <String, BatchItem>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = BatchItem.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of BatchItem-objects as value to a dart map
  static Map<String, List<BatchItem>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<BatchItem>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = BatchItem.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
    'captured_at',
    'excerpt',
    'proposer',
    'generation',
    'status',
    'pre_acceptable',
  };
}

