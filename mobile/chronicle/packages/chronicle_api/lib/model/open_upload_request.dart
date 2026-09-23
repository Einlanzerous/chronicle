//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class OpenUploadRequest {
  /// Returns a new [OpenUploadRequest] instance.
  OpenUploadRequest({
    required this.idempotencyKey,
    required this.contentHash,
    required this.byteSize,
    this.retention,
    this.originalFilename,
    this.recordedAt,
  });

  /// Minted per capture and persisted by the client BEFORE the request goes out, so an HTTP retry is a replay rather than a second memo. 
  String idempotencyKey;

  /// SHA-256 of the file, lowercase hex. Checked on completion: bytes that do not match it are discarded rather than stored as a memo nobody can verify. 
  String contentHash;

  /// Minimum value: 1
  int byteSize;

  /// Omitted means the deployment default. `days_30` is pruned by policy once a durable transcript exists — never on the calendar alone, because pruning audio whose transcription never succeeded is unrecoverable loss with no user-visible warning. 
  OpenUploadRequestRetentionEnum? retention;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? originalFilename;

  /// When a person says this was recorded — offline capture's answer to `captured_at` being arrival time, not recording time. Asserted by the client and never verified: it carries no retention weight, and `CHRN-22`'s pruner reads `captured_at` alone. Display only, and once set on a memo it is as immutable as `captured_at` — a replay or a second delivery path never revises it (CHRN-18 §4, CHRN-118). 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? recordedAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is OpenUploadRequest &&
    other.idempotencyKey == idempotencyKey &&
    other.contentHash == contentHash &&
    other.byteSize == byteSize &&
    other.retention == retention &&
    other.originalFilename == originalFilename &&
    other.recordedAt == recordedAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (idempotencyKey.hashCode) +
    (contentHash.hashCode) +
    (byteSize.hashCode) +
    (retention == null ? 0 : retention!.hashCode) +
    (originalFilename == null ? 0 : originalFilename!.hashCode) +
    (recordedAt == null ? 0 : recordedAt!.hashCode);

  @override
  String toString() => 'OpenUploadRequest[idempotencyKey=$idempotencyKey, contentHash=$contentHash, byteSize=$byteSize, retention=$retention, originalFilename=$originalFilename, recordedAt=$recordedAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'idempotency_key'] = this.idempotencyKey;
      json[r'content_hash'] = this.contentHash;
      json[r'byte_size'] = this.byteSize;
    if (this.retention != null) {
      json[r'retention'] = this.retention;
    } else {
      json[r'retention'] = null;
    }
    if (this.originalFilename != null) {
      json[r'original_filename'] = this.originalFilename;
    } else {
      json[r'original_filename'] = null;
    }
    if (this.recordedAt != null) {
      json[r'recorded_at'] = this.recordedAt!.toUtc().toIso8601String();
    } else {
      json[r'recorded_at'] = null;
    }
    return json;
  }

  /// Returns a new [OpenUploadRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static OpenUploadRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'idempotency_key'), 'Required key "OpenUploadRequest[idempotency_key]" is missing from JSON.');
        assert(json[r'idempotency_key'] != null, 'Required key "OpenUploadRequest[idempotency_key]" has a null value in JSON.');
        assert(json.containsKey(r'content_hash'), 'Required key "OpenUploadRequest[content_hash]" is missing from JSON.');
        assert(json[r'content_hash'] != null, 'Required key "OpenUploadRequest[content_hash]" has a null value in JSON.');
        assert(json.containsKey(r'byte_size'), 'Required key "OpenUploadRequest[byte_size]" is missing from JSON.');
        assert(json[r'byte_size'] != null, 'Required key "OpenUploadRequest[byte_size]" has a null value in JSON.');
        return true;
      }());

      return OpenUploadRequest(
        idempotencyKey: mapValueOfType<String>(json, r'idempotency_key')!,
        contentHash: mapValueOfType<String>(json, r'content_hash')!,
        byteSize: mapValueOfType<int>(json, r'byte_size')!,
        retention: OpenUploadRequestRetentionEnum.fromJson(json[r'retention']),
        originalFilename: mapValueOfType<String>(json, r'original_filename'),
        recordedAt: mapDateTime(json, r'recorded_at', r''),
      );
    }
    return null;
  }

  static List<OpenUploadRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <OpenUploadRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = OpenUploadRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, OpenUploadRequest> mapFromJson(dynamic json) {
    final map = <String, OpenUploadRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = OpenUploadRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of OpenUploadRequest-objects as value to a dart map
  static Map<String, List<OpenUploadRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<OpenUploadRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = OpenUploadRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'idempotency_key',
    'content_hash',
    'byte_size',
  };
}

/// Omitted means the deployment default. `days_30` is pruned by policy once a durable transcript exists — never on the calendar alone, because pruning audio whose transcription never succeeded is unrecoverable loss with no user-visible warning. 
class OpenUploadRequestRetentionEnum {
  /// Instantiate a new enum with the provided [value].
  const OpenUploadRequestRetentionEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const discardNow = OpenUploadRequestRetentionEnum._(r'discard_now');
  static const days30 = OpenUploadRequestRetentionEnum._(r'days_30');
  static const forever = OpenUploadRequestRetentionEnum._(r'forever');

  /// List of all possible values in this [enum][OpenUploadRequestRetentionEnum].
  static const values = <OpenUploadRequestRetentionEnum>[
    discardNow,
    days30,
    forever,
  ];

  static OpenUploadRequestRetentionEnum? fromJson(dynamic value) => OpenUploadRequestRetentionEnumTypeTransformer().decode(value);

  static List<OpenUploadRequestRetentionEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <OpenUploadRequestRetentionEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = OpenUploadRequestRetentionEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [OpenUploadRequestRetentionEnum] to String,
/// and [decode] dynamic data back to [OpenUploadRequestRetentionEnum].
class OpenUploadRequestRetentionEnumTypeTransformer {
  factory OpenUploadRequestRetentionEnumTypeTransformer() => _instance ??= const OpenUploadRequestRetentionEnumTypeTransformer._();

  const OpenUploadRequestRetentionEnumTypeTransformer._();

  String encode(OpenUploadRequestRetentionEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a OpenUploadRequestRetentionEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  OpenUploadRequestRetentionEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'discard_now': return OpenUploadRequestRetentionEnum.discardNow;
        case r'days_30': return OpenUploadRequestRetentionEnum.days30;
        case r'forever': return OpenUploadRequestRetentionEnum.forever;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [OpenUploadRequestRetentionEnumTypeTransformer] instance.
  static OpenUploadRequestRetentionEnumTypeTransformer? _instance;
}


