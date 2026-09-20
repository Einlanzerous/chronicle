//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class WindowReport {
  /// Returns a new [WindowReport] instance.
  WindowReport({
    required this.days,
    required this.memos,
    required this.bytes,
    required this.projectedBytes,
    required this.pctOfProjected,
  });

  int days;

  int memos;

  int bytes;

  /// What the sizing in docs/benchmarks projected for this window.
  int projectedBytes;

  double pctOfProjected;

  @override
  bool operator ==(Object other) => identical(this, other) || other is WindowReport &&
    other.days == days &&
    other.memos == memos &&
    other.bytes == bytes &&
    other.projectedBytes == projectedBytes &&
    other.pctOfProjected == pctOfProjected;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (days.hashCode) +
    (memos.hashCode) +
    (bytes.hashCode) +
    (projectedBytes.hashCode) +
    (pctOfProjected.hashCode);

  @override
  String toString() => 'WindowReport[days=$days, memos=$memos, bytes=$bytes, projectedBytes=$projectedBytes, pctOfProjected=$pctOfProjected]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'days'] = this.days;
      json[r'memos'] = this.memos;
      json[r'bytes'] = this.bytes;
      json[r'projected_bytes'] = this.projectedBytes;
      json[r'pct_of_projected'] = this.pctOfProjected;
    return json;
  }

  /// Returns a new [WindowReport] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static WindowReport? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'days'), 'Required key "WindowReport[days]" is missing from JSON.');
        assert(json[r'days'] != null, 'Required key "WindowReport[days]" has a null value in JSON.');
        assert(json.containsKey(r'memos'), 'Required key "WindowReport[memos]" is missing from JSON.');
        assert(json[r'memos'] != null, 'Required key "WindowReport[memos]" has a null value in JSON.');
        assert(json.containsKey(r'bytes'), 'Required key "WindowReport[bytes]" is missing from JSON.');
        assert(json[r'bytes'] != null, 'Required key "WindowReport[bytes]" has a null value in JSON.');
        assert(json.containsKey(r'projected_bytes'), 'Required key "WindowReport[projected_bytes]" is missing from JSON.');
        assert(json[r'projected_bytes'] != null, 'Required key "WindowReport[projected_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'pct_of_projected'), 'Required key "WindowReport[pct_of_projected]" is missing from JSON.');
        assert(json[r'pct_of_projected'] != null, 'Required key "WindowReport[pct_of_projected]" has a null value in JSON.');
        return true;
      }());

      return WindowReport(
        days: mapValueOfType<int>(json, r'days')!,
        memos: mapValueOfType<int>(json, r'memos')!,
        bytes: mapValueOfType<int>(json, r'bytes')!,
        projectedBytes: mapValueOfType<int>(json, r'projected_bytes')!,
        pctOfProjected: mapValueOfType<double>(json, r'pct_of_projected')!,
      );
    }
    return null;
  }

  static List<WindowReport> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <WindowReport>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = WindowReport.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, WindowReport> mapFromJson(dynamic json) {
    final map = <String, WindowReport>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = WindowReport.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of WindowReport-objects as value to a dart map
  static Map<String, List<WindowReport>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<WindowReport>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = WindowReport.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'days',
    'memos',
    'bytes',
    'projected_bytes',
    'pct_of_projected',
  };
}

