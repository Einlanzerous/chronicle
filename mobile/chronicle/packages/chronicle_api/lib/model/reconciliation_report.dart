//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ReconciliationReport {
  /// Returns a new [ReconciliationReport] instance.
  ReconciliationReport({
    required this.orphans,
    required this.orphanBytes,
    this.orphanSample = const [],
    required this.missing,
    required this.missingBytes,
    this.missingSample = const [],
    required this.mismatched,
    this.mismatchedSample = const [],
  });

  /// Audio on disk that no memo claims.
  int orphans;

  int orphanBytes;

  List<String> orphanSample;

  /// **The direction that matters.** A memo says its audio is present and the file is not there. Non-zero here is the unrecoverable loss CLAUDE.md names as the worst thing this system can do, and it leaves a log line whether or not anybody reads this response. 
  int missing;

  int missingBytes;

  List<String> missingSample;

  int mismatched;

  /// Carries both sizes. Without them a 5-against-4096 truncation and a 4097-against-4096 rounding read identically, and those are not the same finding. 
  List<Mismatch> mismatchedSample;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ReconciliationReport &&
    other.orphans == orphans &&
    other.orphanBytes == orphanBytes &&
    _deepEquality.equals(other.orphanSample, orphanSample) &&
    other.missing == missing &&
    other.missingBytes == missingBytes &&
    _deepEquality.equals(other.missingSample, missingSample) &&
    other.mismatched == mismatched &&
    _deepEquality.equals(other.mismatchedSample, mismatchedSample);

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (orphans.hashCode) +
    (orphanBytes.hashCode) +
    (orphanSample.hashCode) +
    (missing.hashCode) +
    (missingBytes.hashCode) +
    (missingSample.hashCode) +
    (mismatched.hashCode) +
    (mismatchedSample.hashCode);

  @override
  String toString() => 'ReconciliationReport[orphans=$orphans, orphanBytes=$orphanBytes, orphanSample=$orphanSample, missing=$missing, missingBytes=$missingBytes, missingSample=$missingSample, mismatched=$mismatched, mismatchedSample=$mismatchedSample]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'orphans'] = this.orphans;
      json[r'orphan_bytes'] = this.orphanBytes;
      json[r'orphan_sample'] = this.orphanSample;
      json[r'missing'] = this.missing;
      json[r'missing_bytes'] = this.missingBytes;
      json[r'missing_sample'] = this.missingSample;
      json[r'mismatched'] = this.mismatched;
      json[r'mismatched_sample'] = this.mismatchedSample;
    return json;
  }

  /// Returns a new [ReconciliationReport] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ReconciliationReport? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'orphans'), 'Required key "ReconciliationReport[orphans]" is missing from JSON.');
        assert(json[r'orphans'] != null, 'Required key "ReconciliationReport[orphans]" has a null value in JSON.');
        assert(json.containsKey(r'orphan_bytes'), 'Required key "ReconciliationReport[orphan_bytes]" is missing from JSON.');
        assert(json[r'orphan_bytes'] != null, 'Required key "ReconciliationReport[orphan_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'missing'), 'Required key "ReconciliationReport[missing]" is missing from JSON.');
        assert(json[r'missing'] != null, 'Required key "ReconciliationReport[missing]" has a null value in JSON.');
        assert(json.containsKey(r'missing_bytes'), 'Required key "ReconciliationReport[missing_bytes]" is missing from JSON.');
        assert(json[r'missing_bytes'] != null, 'Required key "ReconciliationReport[missing_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'mismatched'), 'Required key "ReconciliationReport[mismatched]" is missing from JSON.');
        assert(json[r'mismatched'] != null, 'Required key "ReconciliationReport[mismatched]" has a null value in JSON.');
        return true;
      }());

      return ReconciliationReport(
        orphans: mapValueOfType<int>(json, r'orphans')!,
        orphanBytes: mapValueOfType<int>(json, r'orphan_bytes')!,
        orphanSample: json[r'orphan_sample'] is Iterable
            ? (json[r'orphan_sample'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        missing: mapValueOfType<int>(json, r'missing')!,
        missingBytes: mapValueOfType<int>(json, r'missing_bytes')!,
        missingSample: json[r'missing_sample'] is Iterable
            ? (json[r'missing_sample'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        mismatched: mapValueOfType<int>(json, r'mismatched')!,
        mismatchedSample: Mismatch.listFromJson(json[r'mismatched_sample']),
      );
    }
    return null;
  }

  static List<ReconciliationReport> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ReconciliationReport>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ReconciliationReport.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ReconciliationReport> mapFromJson(dynamic json) {
    final map = <String, ReconciliationReport>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ReconciliationReport.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ReconciliationReport-objects as value to a dart map
  static Map<String, List<ReconciliationReport>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ReconciliationReport>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ReconciliationReport.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'orphans',
    'orphan_bytes',
    'missing',
    'missing_bytes',
    'mismatched',
  };
}

