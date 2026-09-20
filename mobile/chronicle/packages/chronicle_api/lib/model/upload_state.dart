//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class UploadState {
  /// Returns a new [UploadState] instance.
  UploadState({
    required this.status,
    this.uploadId,
    required this.byteSize,
    required this.offset,
    this.expiresAt,
    this.memo,
    required this.duplicate,
  });

  /// `incomplete` — more bytes are expected, and `offset` says from where. `complete` — `memo` is set and there is no session left to name. There is no third member: a freshly opened session is `incomplete` with `offset: 0`, because \"opened\" and \"opened and nothing received\" are the same fact and a client that had to tell them apart would be switching on a distinction the server does not make. 
  UploadStateStatusEnum status;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? uploadId;

  int byteSize;

  /// How many bytes the server holds. Send from here next.
  int offset;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  DateTime? expiresAt;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  Memo? memo;

  /// Whether this declaration matched a memo that already exists. A replay is a success, not a conflict. 
  bool duplicate;

  @override
  bool operator ==(Object other) => identical(this, other) || other is UploadState &&
    other.status == status &&
    other.uploadId == uploadId &&
    other.byteSize == byteSize &&
    other.offset == offset &&
    other.expiresAt == expiresAt &&
    other.memo == memo &&
    other.duplicate == duplicate;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (status.hashCode) +
    (uploadId == null ? 0 : uploadId!.hashCode) +
    (byteSize.hashCode) +
    (offset.hashCode) +
    (expiresAt == null ? 0 : expiresAt!.hashCode) +
    (memo == null ? 0 : memo!.hashCode) +
    (duplicate.hashCode);

  @override
  String toString() => 'UploadState[status=$status, uploadId=$uploadId, byteSize=$byteSize, offset=$offset, expiresAt=$expiresAt, memo=$memo, duplicate=$duplicate]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'status'] = this.status;
    if (this.uploadId != null) {
      json[r'upload_id'] = this.uploadId;
    } else {
      json[r'upload_id'] = null;
    }
      json[r'byte_size'] = this.byteSize;
      json[r'offset'] = this.offset;
    if (this.expiresAt != null) {
      json[r'expires_at'] = this.expiresAt!.toUtc().toIso8601String();
    } else {
      json[r'expires_at'] = null;
    }
    if (this.memo != null) {
      json[r'memo'] = this.memo;
    } else {
      json[r'memo'] = null;
    }
      json[r'duplicate'] = this.duplicate;
    return json;
  }

  /// Returns a new [UploadState] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static UploadState? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'status'), 'Required key "UploadState[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "UploadState[status]" has a null value in JSON.');
        assert(json.containsKey(r'byte_size'), 'Required key "UploadState[byte_size]" is missing from JSON.');
        assert(json[r'byte_size'] != null, 'Required key "UploadState[byte_size]" has a null value in JSON.');
        assert(json.containsKey(r'offset'), 'Required key "UploadState[offset]" is missing from JSON.');
        assert(json[r'offset'] != null, 'Required key "UploadState[offset]" has a null value in JSON.');
        assert(json.containsKey(r'duplicate'), 'Required key "UploadState[duplicate]" is missing from JSON.');
        assert(json[r'duplicate'] != null, 'Required key "UploadState[duplicate]" has a null value in JSON.');
        return true;
      }());

      return UploadState(
        status: UploadStateStatusEnum.fromJson(json[r'status'])!,
        uploadId: mapValueOfType<String>(json, r'upload_id'),
        byteSize: mapValueOfType<int>(json, r'byte_size')!,
        offset: mapValueOfType<int>(json, r'offset')!,
        expiresAt: mapDateTime(json, r'expires_at', r''),
        memo: Memo.fromJson(json[r'memo']),
        duplicate: mapValueOfType<bool>(json, r'duplicate')!,
      );
    }
    return null;
  }

  static List<UploadState> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <UploadState>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = UploadState.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, UploadState> mapFromJson(dynamic json) {
    final map = <String, UploadState>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = UploadState.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of UploadState-objects as value to a dart map
  static Map<String, List<UploadState>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<UploadState>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = UploadState.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'status',
    'byte_size',
    'offset',
    'duplicate',
  };
}

/// `incomplete` — more bytes are expected, and `offset` says from where. `complete` — `memo` is set and there is no session left to name. There is no third member: a freshly opened session is `incomplete` with `offset: 0`, because \"opened\" and \"opened and nothing received\" are the same fact and a client that had to tell them apart would be switching on a distinction the server does not make. 
class UploadStateStatusEnum {
  /// Instantiate a new enum with the provided [value].
  const UploadStateStatusEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const incomplete = UploadStateStatusEnum._(r'incomplete');
  static const complete = UploadStateStatusEnum._(r'complete');

  /// List of all possible values in this [enum][UploadStateStatusEnum].
  static const values = <UploadStateStatusEnum>[
    incomplete,
    complete,
  ];

  static UploadStateStatusEnum? fromJson(dynamic value) => UploadStateStatusEnumTypeTransformer().decode(value);

  static List<UploadStateStatusEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <UploadStateStatusEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = UploadStateStatusEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [UploadStateStatusEnum] to String,
/// and [decode] dynamic data back to [UploadStateStatusEnum].
class UploadStateStatusEnumTypeTransformer {
  factory UploadStateStatusEnumTypeTransformer() => _instance ??= const UploadStateStatusEnumTypeTransformer._();

  const UploadStateStatusEnumTypeTransformer._();

  String encode(UploadStateStatusEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a UploadStateStatusEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  UploadStateStatusEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'incomplete': return UploadStateStatusEnum.incomplete;
        case r'complete': return UploadStateStatusEnum.complete;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [UploadStateStatusEnumTypeTransformer] instance.
  static UploadStateStatusEnumTypeTransformer? _instance;
}


