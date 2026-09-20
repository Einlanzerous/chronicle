//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class StorageReport {
  /// Returns a new [StorageReport] instance.
  StorageReport({
    required this.root,
    required this.disk,
    required this.corpus,
    required this.window,
    required this.reconciliation,
  });

  /// The audio store's root on disk.
  String root;

  DiskReport disk;

  CorpusReport corpus;

  WindowReport window;

  ReconciliationReport reconciliation;

  @override
  bool operator ==(Object other) => identical(this, other) || other is StorageReport &&
    other.root == root &&
    other.disk == disk &&
    other.corpus == corpus &&
    other.window == window &&
    other.reconciliation == reconciliation;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (root.hashCode) +
    (disk.hashCode) +
    (corpus.hashCode) +
    (window.hashCode) +
    (reconciliation.hashCode);

  @override
  String toString() => 'StorageReport[root=$root, disk=$disk, corpus=$corpus, window=$window, reconciliation=$reconciliation]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'root'] = this.root;
      json[r'disk'] = this.disk;
      json[r'corpus'] = this.corpus;
      json[r'window'] = this.window;
      json[r'reconciliation'] = this.reconciliation;
    return json;
  }

  /// Returns a new [StorageReport] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static StorageReport? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'root'), 'Required key "StorageReport[root]" is missing from JSON.');
        assert(json[r'root'] != null, 'Required key "StorageReport[root]" has a null value in JSON.');
        assert(json.containsKey(r'disk'), 'Required key "StorageReport[disk]" is missing from JSON.');
        assert(json[r'disk'] != null, 'Required key "StorageReport[disk]" has a null value in JSON.');
        assert(json.containsKey(r'corpus'), 'Required key "StorageReport[corpus]" is missing from JSON.');
        assert(json[r'corpus'] != null, 'Required key "StorageReport[corpus]" has a null value in JSON.');
        assert(json.containsKey(r'window'), 'Required key "StorageReport[window]" is missing from JSON.');
        assert(json[r'window'] != null, 'Required key "StorageReport[window]" has a null value in JSON.');
        assert(json.containsKey(r'reconciliation'), 'Required key "StorageReport[reconciliation]" is missing from JSON.');
        assert(json[r'reconciliation'] != null, 'Required key "StorageReport[reconciliation]" has a null value in JSON.');
        return true;
      }());

      return StorageReport(
        root: mapValueOfType<String>(json, r'root')!,
        disk: DiskReport.fromJson(json[r'disk'])!,
        corpus: CorpusReport.fromJson(json[r'corpus'])!,
        window: WindowReport.fromJson(json[r'window'])!,
        reconciliation: ReconciliationReport.fromJson(json[r'reconciliation'])!,
      );
    }
    return null;
  }

  static List<StorageReport> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <StorageReport>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = StorageReport.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, StorageReport> mapFromJson(dynamic json) {
    final map = <String, StorageReport>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = StorageReport.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of StorageReport-objects as value to a dart map
  static Map<String, List<StorageReport>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<StorageReport>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = StorageReport.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'root',
    'disk',
    'corpus',
    'window',
    'reconciliation',
  };
}

