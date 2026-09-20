//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class DiskReport {
  /// Returns a new [DiskReport] instance.
  DiskReport({
    required this.files,
    required this.bytes,
    required this.strays,
    required this.strayBytes,
    this.straySample = const [],
    required this.staging,
    required this.stagingBytes,
    required this.volumeKnown,
    required this.volumeTotalBytes,
    required this.volumeFreeBytes,
  });

  int files;

  int bytes;

  /// Files under the root that this layout did not write.
  int strays;

  int strayBytes;

  /// A sample of the stray paths. Strays are never counted as corpus and never offered to the pruner. 
  List<String> straySample;

  /// Uploads in flight (CHRN-20) — files this service wrote and understands, which are not recordings yet and may never become any. Reported apart from both corpus and strays: counted as corpus it would inflate what the memos cost, and counted as strays every phone mid-upload would read as a file nobody can name. 
  int staging;

  int stagingBytes;

  /// Whether the volume figures below were measurable. It is what separates \"not measured\" from \"measured as zero\" — a full volume has `volume_free_bytes: 0`, which is exactly when the figure matters most, so neither number is ever omitted. 
  bool volumeKnown;

  int volumeTotalBytes;

  int volumeFreeBytes;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DiskReport &&
    other.files == files &&
    other.bytes == bytes &&
    other.strays == strays &&
    other.strayBytes == strayBytes &&
    _deepEquality.equals(other.straySample, straySample) &&
    other.staging == staging &&
    other.stagingBytes == stagingBytes &&
    other.volumeKnown == volumeKnown &&
    other.volumeTotalBytes == volumeTotalBytes &&
    other.volumeFreeBytes == volumeFreeBytes;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (files.hashCode) +
    (bytes.hashCode) +
    (strays.hashCode) +
    (strayBytes.hashCode) +
    (straySample.hashCode) +
    (staging.hashCode) +
    (stagingBytes.hashCode) +
    (volumeKnown.hashCode) +
    (volumeTotalBytes.hashCode) +
    (volumeFreeBytes.hashCode);

  @override
  String toString() => 'DiskReport[files=$files, bytes=$bytes, strays=$strays, strayBytes=$strayBytes, straySample=$straySample, staging=$staging, stagingBytes=$stagingBytes, volumeKnown=$volumeKnown, volumeTotalBytes=$volumeTotalBytes, volumeFreeBytes=$volumeFreeBytes]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'files'] = this.files;
      json[r'bytes'] = this.bytes;
      json[r'strays'] = this.strays;
      json[r'stray_bytes'] = this.strayBytes;
      json[r'stray_sample'] = this.straySample;
      json[r'staging'] = this.staging;
      json[r'staging_bytes'] = this.stagingBytes;
      json[r'volume_known'] = this.volumeKnown;
      json[r'volume_total_bytes'] = this.volumeTotalBytes;
      json[r'volume_free_bytes'] = this.volumeFreeBytes;
    return json;
  }

  /// Returns a new [DiskReport] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DiskReport? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'files'), 'Required key "DiskReport[files]" is missing from JSON.');
        assert(json[r'files'] != null, 'Required key "DiskReport[files]" has a null value in JSON.');
        assert(json.containsKey(r'bytes'), 'Required key "DiskReport[bytes]" is missing from JSON.');
        assert(json[r'bytes'] != null, 'Required key "DiskReport[bytes]" has a null value in JSON.');
        assert(json.containsKey(r'strays'), 'Required key "DiskReport[strays]" is missing from JSON.');
        assert(json[r'strays'] != null, 'Required key "DiskReport[strays]" has a null value in JSON.');
        assert(json.containsKey(r'stray_bytes'), 'Required key "DiskReport[stray_bytes]" is missing from JSON.');
        assert(json[r'stray_bytes'] != null, 'Required key "DiskReport[stray_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'staging'), 'Required key "DiskReport[staging]" is missing from JSON.');
        assert(json[r'staging'] != null, 'Required key "DiskReport[staging]" has a null value in JSON.');
        assert(json.containsKey(r'staging_bytes'), 'Required key "DiskReport[staging_bytes]" is missing from JSON.');
        assert(json[r'staging_bytes'] != null, 'Required key "DiskReport[staging_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'volume_known'), 'Required key "DiskReport[volume_known]" is missing from JSON.');
        assert(json[r'volume_known'] != null, 'Required key "DiskReport[volume_known]" has a null value in JSON.');
        assert(json.containsKey(r'volume_total_bytes'), 'Required key "DiskReport[volume_total_bytes]" is missing from JSON.');
        assert(json[r'volume_total_bytes'] != null, 'Required key "DiskReport[volume_total_bytes]" has a null value in JSON.');
        assert(json.containsKey(r'volume_free_bytes'), 'Required key "DiskReport[volume_free_bytes]" is missing from JSON.');
        assert(json[r'volume_free_bytes'] != null, 'Required key "DiskReport[volume_free_bytes]" has a null value in JSON.');
        return true;
      }());

      return DiskReport(
        files: mapValueOfType<int>(json, r'files')!,
        bytes: mapValueOfType<int>(json, r'bytes')!,
        strays: mapValueOfType<int>(json, r'strays')!,
        strayBytes: mapValueOfType<int>(json, r'stray_bytes')!,
        straySample: json[r'stray_sample'] is Iterable
            ? (json[r'stray_sample'] as Iterable).cast<String>().toList(growable: false)
            : const [],
        staging: mapValueOfType<int>(json, r'staging')!,
        stagingBytes: mapValueOfType<int>(json, r'staging_bytes')!,
        volumeKnown: mapValueOfType<bool>(json, r'volume_known')!,
        volumeTotalBytes: mapValueOfType<int>(json, r'volume_total_bytes')!,
        volumeFreeBytes: mapValueOfType<int>(json, r'volume_free_bytes')!,
      );
    }
    return null;
  }

  static List<DiskReport> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DiskReport>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DiskReport.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DiskReport> mapFromJson(dynamic json) {
    final map = <String, DiskReport>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DiskReport.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DiskReport-objects as value to a dart map
  static Map<String, List<DiskReport>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DiskReport>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DiskReport.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'files',
    'bytes',
    'strays',
    'stray_bytes',
    'staging',
    'staging_bytes',
    'volume_known',
    'volume_total_bytes',
    'volume_free_bytes',
  };
}

