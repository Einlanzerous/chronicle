//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class TriageReport {
  /// Returns a new [TriageReport] instance.
  TriageReport({
    required this.backlog,
    required this.deferred_,
    this.inFlight = const [],
    this.unresolved = const [],
    this.ambiguous = const [],
    this.refused = const [],
  });

  BacklogReport backlog;

  int deferred_;

  List<LinkState> inFlight;

  /// The decision wrote tier 2 and the ticket never arrived.
  List<LinkState> unresolved;

  /// More than one ticket claims this memo, and nothing will guess.
  List<LinkState> ambiguous;

  /// Switchyard said no, and caches that answer.
  List<LinkState> refused;

  @override
  bool operator ==(Object other) => identical(this, other) || other is TriageReport &&
    other.backlog == backlog &&
    other.deferred_ == deferred_ &&
    _deepEquality.equals(other.inFlight, inFlight) &&
    _deepEquality.equals(other.unresolved, unresolved) &&
    _deepEquality.equals(other.ambiguous, ambiguous) &&
    _deepEquality.equals(other.refused, refused);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (backlog.hashCode) +
    (deferred_.hashCode) +
    (inFlight.hashCode) +
    (unresolved.hashCode) +
    (ambiguous.hashCode) +
    (refused.hashCode);

  @override
  String toString() => 'TriageReport[backlog=$backlog, deferred_=$deferred_, inFlight=$inFlight, unresolved=$unresolved, ambiguous=$ambiguous, refused=$refused]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'backlog'] = this.backlog;
      json[r'deferred'] = this.deferred_;
      json[r'in_flight'] = this.inFlight;
      json[r'unresolved'] = this.unresolved;
      json[r'ambiguous'] = this.ambiguous;
      json[r'refused'] = this.refused;
    return json;
  }

  /// Returns a new [TriageReport] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static TriageReport? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'backlog'), 'Required key "TriageReport[backlog]" is missing from JSON.');
        assert(json[r'backlog'] != null, 'Required key "TriageReport[backlog]" has a null value in JSON.');
        assert(json.containsKey(r'deferred'), 'Required key "TriageReport[deferred]" is missing from JSON.');
        assert(json[r'deferred'] != null, 'Required key "TriageReport[deferred]" has a null value in JSON.');
        assert(json.containsKey(r'in_flight'), 'Required key "TriageReport[in_flight]" is missing from JSON.');
        assert(json[r'in_flight'] != null, 'Required key "TriageReport[in_flight]" has a null value in JSON.');
        assert(json.containsKey(r'unresolved'), 'Required key "TriageReport[unresolved]" is missing from JSON.');
        assert(json[r'unresolved'] != null, 'Required key "TriageReport[unresolved]" has a null value in JSON.');
        assert(json.containsKey(r'ambiguous'), 'Required key "TriageReport[ambiguous]" is missing from JSON.');
        assert(json[r'ambiguous'] != null, 'Required key "TriageReport[ambiguous]" has a null value in JSON.');
        assert(json.containsKey(r'refused'), 'Required key "TriageReport[refused]" is missing from JSON.');
        assert(json[r'refused'] != null, 'Required key "TriageReport[refused]" has a null value in JSON.');
        return true;
      }());

      return TriageReport(
        backlog: BacklogReport.fromJson(json[r'backlog'])!,
        deferred_: mapValueOfType<int>(json, r'deferred')!,
        inFlight: LinkState.listFromJson(json[r'in_flight']),
        unresolved: LinkState.listFromJson(json[r'unresolved']),
        ambiguous: LinkState.listFromJson(json[r'ambiguous']),
        refused: LinkState.listFromJson(json[r'refused']),
      );
    }
    return null;
  }

  static List<TriageReport> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <TriageReport>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = TriageReport.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, TriageReport> mapFromJson(dynamic json) {
    final map = <String, TriageReport>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = TriageReport.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of TriageReport-objects as value to a dart map
  static Map<String, List<TriageReport>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<TriageReport>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = TriageReport.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'backlog',
    'deferred',
    'in_flight',
    'unresolved',
    'ambiguous',
    'refused',
  };
}

