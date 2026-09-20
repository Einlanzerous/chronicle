//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class CorpusReport {
  /// Returns a new [CorpusReport] instance.
  CorpusReport({
    required this.memos,
    required this.audioPresent,
    required this.audioPruned,
    required this.recordedBytes,
    required this.everBytes,
    required this.oldestCapture,
    required this.newestCapture,
  });

  int memos;

  int audioPresent;

  int audioPruned;

  int recordedBytes;

  int everBytes;

  /// Null on an empty corpus. Always present, so absent never means empty.
  DateTime? oldestCapture;

  DateTime? newestCapture;

  @override
  bool operator ==(Object other) => identical(this, other) || other is CorpusReport &&
    other.memos == memos &&
    other.audioPresent == audioPresent &&
    other.audioPruned == audioPruned &&
    other.recordedBytes == recordedBytes &&
    other.everBytes == everBytes &&
    other.oldestCapture == oldestCapture &&
    other.newestCapture == newestCapture;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memos.hashCode) +
    (audioPresent.hashCode) +
    (audioPruned.hashCode) +
    (recordedBytes.hashCode) +
    (everBytes.hashCode) +
    (oldestCapture == null ? 0 : oldestCapture!.hashCode) +
    (newestCapture == null ? 0 : newestCapture!.hashCode);

  @override
  String toString() => 'CorpusReport[memos=$memos, audioPresent=$audioPresent, audioPruned=$audioPruned, recordedBytes=$recordedBytes, everBytes=$everBytes, oldestCapture=$oldestCapture, newestCapture=$newestCapture]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memos'] = this.memos;
      json[r'audio_present'] = this.audioPresent;
      json[r'audio_pruned'] = this.audioPruned;
      json[r'recorded_bytes'] = this.recordedBytes;
      json[r'ever_bytes'] = this.everBytes;
    if (this.oldestCapture != null) {
      json[r'oldest_capture'] = this.oldestCapture!.toUtc().toIso8601String();
    } else {
      json[r'oldest_capture'] = null;
    }
    if (this.newestCapture != null) {
      json[r'newest_capture'] = this.newestCapture!.toUtc().toIso8601String();
    } else {
      json[r'newest_capture'] = null;
    }
    return json;
  }

  /// Returns a new [CorpusReport] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static CorpusReport? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memos'), 'Required key "CorpusReport[memos]" is missing from JSON.');
        assert(json[r'memos'] != null, 'Required key "CorpusReport[memos]" has a null value in JSON.');
        assert(json.containsKey(r'audio_present'), 'Required key "CorpusReport[audio_present]" is missing from JSON.');
        assert(json[r'audio_present'] != null, 'Required key "CorpusReport[audio_present]" has a null value in JSON.');
        assert(json.containsKey(r'audio_pruned'), 'Required key "CorpusReport[audio_pruned]" is missing from JSON.');
        assert(json[r'audio_pruned'] != null, 'Required key "CorpusReport[audio_pruned]" has a null value in JSON.');
        assert(json.containsKey(r'recorded_bytes'), 'Required key "CorpusReport[recorded_bytes]" is missing from JSON.');
        assert(json[r'recorded_bytes'] != null, 'Required key "CorpusReport[recorded_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'ever_bytes'), 'Required key "CorpusReport[ever_bytes]" is missing from JSON.');
        assert(json[r'ever_bytes'] != null, 'Required key "CorpusReport[ever_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'oldest_capture'), 'Required key "CorpusReport[oldest_capture]" is missing from JSON.');
        assert(json.containsKey(r'newest_capture'), 'Required key "CorpusReport[newest_capture]" is missing from JSON.');
        return true;
      }());

      return CorpusReport(
        memos: mapValueOfType<int>(json, r'memos')!,
        audioPresent: mapValueOfType<int>(json, r'audio_present')!,
        audioPruned: mapValueOfType<int>(json, r'audio_pruned')!,
        recordedBytes: mapValueOfType<int>(json, r'recorded_bytes')!,
        everBytes: mapValueOfType<int>(json, r'ever_bytes')!,
        oldestCapture: mapDateTime(json, r'oldest_capture', r''),
        newestCapture: mapDateTime(json, r'newest_capture', r''),
      );
    }
    return null;
  }

  static List<CorpusReport> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <CorpusReport>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = CorpusReport.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, CorpusReport> mapFromJson(dynamic json) {
    final map = <String, CorpusReport>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = CorpusReport.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of CorpusReport-objects as value to a dart map
  static Map<String, List<CorpusReport>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<CorpusReport>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = CorpusReport.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memos',
    'audio_present',
    'audio_pruned',
    'recorded_bytes',
    'ever_bytes',
    'oldest_capture',
    'newest_capture',
  };
}

