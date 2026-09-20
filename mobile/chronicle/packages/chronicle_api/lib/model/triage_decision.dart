//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TriageDecision {
  /// Returns a new [TriageDecision] instance.
  TriageDecision({
    required this.memoId,
    this.proposer,
    this.generation,
    this.proposalOverride,
    this.confirmEdit,
  });

  String memoId;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? proposer;

  /// The generation this decision was made against. Sent back so the server can refuse a decision made against a proposal that has since been regenerated. 
  int? generation;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Override? proposalOverride;

  /// Whether the person edited the proposal before confirming. Recorded because \"agreed\" and \"agreed after rewriting it\" are different facts about the model, and CHRN-36's eval reads them apart.  A changed destination or title is an `override`, not a field here: the decision says which proposal it is about, and the override says what the person altered about it. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  bool? confirmEdit;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TriageDecision &&
    other.memoId == memoId &&
    other.proposer == proposer &&
    other.generation == generation &&
    other.proposalOverride == proposalOverride &&
    other.confirmEdit == confirmEdit;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode) +
    (proposer == null ? 0 : proposer!.hashCode) +
    (generation == null ? 0 : generation!.hashCode) +
    (proposalOverride == null ? 0 : proposalOverride!.hashCode) +
    (confirmEdit == null ? 0 : confirmEdit!.hashCode);

  @override
  String toString() => 'TriageDecision[memoId=$memoId, proposer=$proposer, generation=$generation, proposalOverride=$proposalOverride, confirmEdit=$confirmEdit]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
    if (this.proposer != null) {
      json[r'proposer'] = this.proposer;
    } else {
      json[r'proposer'] = null;
    }
    if (this.generation != null) {
      json[r'generation'] = this.generation;
    } else {
      json[r'generation'] = null;
    }
    if (this.proposalOverride != null) {
      json[r'override'] = this.proposalOverride;
    } else {
      json[r'override'] = null;
    }
    if (this.confirmEdit != null) {
      json[r'confirm_edit'] = this.confirmEdit;
    } else {
      json[r'confirm_edit'] = null;
    }
    return json;
  }

  /// Returns a new [TriageDecision] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TriageDecision? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "TriageDecision[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "TriageDecision[memo_id]" has a null value in JSON.');
        return true;
      }());

      return TriageDecision(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        proposer: mapValueOfType<String>(json, r'proposer'),
        generation: mapValueOfType<int>(json, r'generation'),
        proposalOverride: Override.fromJson(json[r'override']),
        confirmEdit: mapValueOfType<bool>(json, r'confirm_edit'),
      );
    }
    return null;
  }

  static List<TriageDecision> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TriageDecision>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TriageDecision.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TriageDecision> mapFromJson(dynamic json) {
    final map = <String, TriageDecision>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TriageDecision.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TriageDecision-objects as value to a dart map
  static Map<String, List<TriageDecision>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TriageDecision>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TriageDecision.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
  };
}

