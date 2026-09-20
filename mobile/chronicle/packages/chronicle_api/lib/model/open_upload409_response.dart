//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class OpenUpload409Response {
  /// Returns a new [OpenUpload409Response] instance.
  OpenUpload409Response({
    required this.status,
    this.uploadId,
    required this.byteSize,
    required this.offset,
    this.expiresAt,
    this.memo,
    required this.duplicate,
    required this.code,
    required this.message,
  });

  /// `incomplete` — more bytes are expected, and `offset` says from where. `complete` — `memo` is set and there is no session left to name. There is no third member: a freshly opened session is `incomplete` with `offset: 0`, because \"opened\" and \"opened and nothing received\" are the same fact and a client that had to tell them apart would be switching on a distinction the server does not make. 
  OpenUpload409ResponseStatusEnum status;

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

  /// A stable machine token. Clients branch on this, never on `message`.
  String code;

  /// One sentence, for a person. Not stable across releases.
  String message;

  @override
  bool operator ==(Object other) => identical(this, other) || other is OpenUpload409Response &&
    other.status == status &&
    other.uploadId == uploadId &&
    other.byteSize == byteSize &&
    other.offset == offset &&
    other.expiresAt == expiresAt &&
    other.memo == memo &&
    other.duplicate == duplicate &&
    other.code == code &&
    other.message == message;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (status.hashCode) +
    (uploadId == null ? 0 : uploadId!.hashCode) +
    (byteSize.hashCode) +
    (offset.hashCode) +
    (expiresAt == null ? 0 : expiresAt!.hashCode) +
    (memo == null ? 0 : memo!.hashCode) +
    (duplicate.hashCode) +
    (code.hashCode) +
    (message.hashCode);

  @override
  String toString() => 'OpenUpload409Response[status=$status, uploadId=$uploadId, byteSize=$byteSize, offset=$offset, expiresAt=$expiresAt, memo=$memo, duplicate=$duplicate, code=$code, message=$message]';

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
      json[r'code'] = this.code;
      json[r'message'] = this.message;
    return json;
  }

  /// Returns a new [OpenUpload409Response] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static OpenUpload409Response? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'status'), 'Required key "OpenUpload409Response[status]" is missing from JSON.');
        assert(json[r'status'] != null, 'Required key "OpenUpload409Response[status]" has a null value in JSON.');
        assert(json.containsKey(r'byte_size'), 'Required key "OpenUpload409Response[byte_size]" is missing from JSON.');
        assert(json[r'byte_size'] != null, 'Required key "OpenUpload409Response[byte_size]" has a null value in JSON.');
        assert(json.containsKey(r'offset'), 'Required key "OpenUpload409Response[offset]" is missing from JSON.');
        assert(json[r'offset'] != null, 'Required key "OpenUpload409Response[offset]" has a null value in JSON.');
        assert(json.containsKey(r'duplicate'), 'Required key "OpenUpload409Response[duplicate]" is missing from JSON.');
        assert(json[r'duplicate'] != null, 'Required key "OpenUpload409Response[duplicate]" has a null value in JSON.');
        assert(json.containsKey(r'code'), 'Required key "OpenUpload409Response[code]" is missing from JSON.');
        assert(json[r'code'] != null, 'Required key "OpenUpload409Response[code]" has a null value in JSON.');
        assert(json.containsKey(r'message'), 'Required key "OpenUpload409Response[message]" is missing from JSON.');
        assert(json[r'message'] != null, 'Required key "OpenUpload409Response[message]" has a null value in JSON.');
        return true;
      }());

      return OpenUpload409Response(
        status: OpenUpload409ResponseStatusEnum.fromJson(json[r'status'])!,
        uploadId: mapValueOfType<String>(json, r'upload_id'),
        byteSize: mapValueOfType<int>(json, r'byte_size')!,
        offset: mapValueOfType<int>(json, r'offset')!,
        expiresAt: mapDateTime(json, r'expires_at', r''),
        memo: Memo.fromJson(json[r'memo']),
        duplicate: mapValueOfType<bool>(json, r'duplicate')!,
        code: mapValueOfType<String>(json, r'code')!,
        message: mapValueOfType<String>(json, r'message')!,
      );
    }
    return null;
  }

  static List<OpenUpload409Response> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <OpenUpload409Response>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = OpenUpload409Response.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, OpenUpload409Response> mapFromJson(dynamic json) {
    final map = <String, OpenUpload409Response>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = OpenUpload409Response.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of OpenUpload409Response-objects as value to a dart map
  static Map<String, List<OpenUpload409Response>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<OpenUpload409Response>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = OpenUpload409Response.listFromJson(entry.value, growable: growable,);
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
    'code',
    'message',
  };
}

/// `incomplete` — more bytes are expected, and `offset` says from where. `complete` — `memo` is set and there is no session left to name. There is no third member: a freshly opened session is `incomplete` with `offset: 0`, because \"opened\" and \"opened and nothing received\" are the same fact and a client that had to tell them apart would be switching on a distinction the server does not make. 
class OpenUpload409ResponseStatusEnum {
  /// Instantiate a new enum with the provided [value].
  const OpenUpload409ResponseStatusEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const incomplete = OpenUpload409ResponseStatusEnum._(r'incomplete');
  static const complete = OpenUpload409ResponseStatusEnum._(r'complete');

  /// List of all possible values in this [enum][OpenUpload409ResponseStatusEnum].
  static const values = <OpenUpload409ResponseStatusEnum>[
    incomplete,
    complete,
  ];

  static OpenUpload409ResponseStatusEnum? fromJson(dynamic value) => OpenUpload409ResponseStatusEnumTypeTransformer().decode(value);

  static List<OpenUpload409ResponseStatusEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <OpenUpload409ResponseStatusEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = OpenUpload409ResponseStatusEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [OpenUpload409ResponseStatusEnum] to String,
/// and [decode] dynamic data back to [OpenUpload409ResponseStatusEnum].
class OpenUpload409ResponseStatusEnumTypeTransformer {
  factory OpenUpload409ResponseStatusEnumTypeTransformer() => _instance ??= const OpenUpload409ResponseStatusEnumTypeTransformer._();

  const OpenUpload409ResponseStatusEnumTypeTransformer._();

  String encode(OpenUpload409ResponseStatusEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a OpenUpload409ResponseStatusEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  OpenUpload409ResponseStatusEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'incomplete': return OpenUpload409ResponseStatusEnum.incomplete;
        case r'complete': return OpenUpload409ResponseStatusEnum.complete;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [OpenUpload409ResponseStatusEnumTypeTransformer] instance.
  static OpenUpload409ResponseStatusEnumTypeTransformer? _instance;
}


