//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Memo {
  /// Returns a new [Memo] instance.
  Memo({
    required this.id,
    required this.state,
    required this.retention,
    required this.contentHash,
    required this.byteSize,
    required this.capturedAt,
    required this.recordedAt,
    required this.audioPruned,
    required this.retentionStatus,
    required this.prunesAt,
    required this.audioPrunedAt,
    required this.durationMs,
    required this.codec,
    required this.sampleRateHz,
    this.originalFilename,
  });

  String id;

  /// captured, queued, transcribing, transcribed, held, discarded.
  String state;

  MemoRetentionEnum retention;

  String contentHash;

  int byteSize;

  DateTime capturedAt;

  /// When a person says this was recorded, client-asserted and never verified. Null for a memo whose arrival never sent one — every memo captured before CHRN-118, and any watcher delivery. Display only: it carries no retention weight and is not read by the pruner or by `prunes_at`, which stay on `captured_at`. 
  DateTime? recordedAt;

  /// The recording is gone and the transcript remains. Never true without a durable transcript — that predicate, not the calendar, is what gates deletion. 
  bool audioPruned;

  /// What will happen to this memo's audio — the same clause the pruner sweeps with.
  String retentionStatus;

  /// When, on that clause — and **null unless `retention_status` is `scheduled`** (CHRN-107 ruling 7). It used to carry `audio_pruned_at` on a pruned memo, because `store.RetentionStatus` overloads one `at` across both cases and this field took it unexamined: a field named `prunes_at` answering a date in the past, on exactly the memo a person is most likely to be looking at when they wonder what happened to it. The past date is `audio_pruned_at` below; this one only ever names a future sweep. 
  DateTime? prunesAt;

  /// When the audio was deleted, and null until it is. Set exactly when `retention_status` is `pruned`, which is also when `audio_pruned` is true — the boolean says whether, this says when. 
  DateTime? audioPrunedAt;

  /// Null until something has decoded the file; a declaration is not a measurement.
  int? durationMs;

  String? codec;

  int? sampleRateHz;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? originalFilename;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Memo &&
    other.id == id &&
    other.state == state &&
    other.retention == retention &&
    other.contentHash == contentHash &&
    other.byteSize == byteSize &&
    other.capturedAt == capturedAt &&
    other.recordedAt == recordedAt &&
    other.audioPruned == audioPruned &&
    other.retentionStatus == retentionStatus &&
    other.prunesAt == prunesAt &&
    other.audioPrunedAt == audioPrunedAt &&
    other.durationMs == durationMs &&
    other.codec == codec &&
    other.sampleRateHz == sampleRateHz &&
    other.originalFilename == originalFilename;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (id.hashCode) +
    (state.hashCode) +
    (retention.hashCode) +
    (contentHash.hashCode) +
    (byteSize.hashCode) +
    (capturedAt.hashCode) +
    (recordedAt == null ? 0 : recordedAt!.hashCode) +
    (audioPruned.hashCode) +
    (retentionStatus.hashCode) +
    (prunesAt == null ? 0 : prunesAt!.hashCode) +
    (audioPrunedAt == null ? 0 : audioPrunedAt!.hashCode) +
    (durationMs == null ? 0 : durationMs!.hashCode) +
    (codec == null ? 0 : codec!.hashCode) +
    (sampleRateHz == null ? 0 : sampleRateHz!.hashCode) +
    (originalFilename == null ? 0 : originalFilename!.hashCode);

  @override
  String toString() => 'Memo[id=$id, state=$state, retention=$retention, contentHash=$contentHash, byteSize=$byteSize, capturedAt=$capturedAt, recordedAt=$recordedAt, audioPruned=$audioPruned, retentionStatus=$retentionStatus, prunesAt=$prunesAt, audioPrunedAt=$audioPrunedAt, durationMs=$durationMs, codec=$codec, sampleRateHz=$sampleRateHz, originalFilename=$originalFilename]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'id'] = this.id;
      json[r'state'] = this.state;
      json[r'retention'] = this.retention;
      json[r'content_hash'] = this.contentHash;
      json[r'byte_size'] = this.byteSize;
      json[r'captured_at'] = this.capturedAt.toUtc().toIso8601String();
    if (this.recordedAt != null) {
      json[r'recorded_at'] = this.recordedAt!.toUtc().toIso8601String();
    } else {
      json[r'recorded_at'] = null;
    }
      json[r'audio_pruned'] = this.audioPruned;
      json[r'retention_status'] = this.retentionStatus;
    if (this.prunesAt != null) {
      json[r'prunes_at'] = this.prunesAt!.toUtc().toIso8601String();
    } else {
      json[r'prunes_at'] = null;
    }
    if (this.audioPrunedAt != null) {
      json[r'audio_pruned_at'] = this.audioPrunedAt!.toUtc().toIso8601String();
    } else {
      json[r'audio_pruned_at'] = null;
    }
    if (this.durationMs != null) {
      json[r'duration_ms'] = this.durationMs;
    } else {
      json[r'duration_ms'] = null;
    }
    if (this.codec != null) {
      json[r'codec'] = this.codec;
    } else {
      json[r'codec'] = null;
    }
    if (this.sampleRateHz != null) {
      json[r'sample_rate_hz'] = this.sampleRateHz;
    } else {
      json[r'sample_rate_hz'] = null;
    }
    if (this.originalFilename != null) {
      json[r'original_filename'] = this.originalFilename;
    } else {
      json[r'original_filename'] = null;
    }
    return json;
  }

  /// Returns a new [Memo] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Memo? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'id'), 'Required key "Memo[id]" is missing from JSON.');
        assert(json[r'id'] != null, 'Required key "Memo[id]" has a null value in JSON.');
        assert(json.containsKey(r'state'), 'Required key "Memo[state]" is missing from JSON.');
        assert(json[r'state'] != null, 'Required key "Memo[state]" has a null value in JSON.');
        assert(json.containsKey(r'retention'), 'Required key "Memo[retention]" is missing from JSON.');
        assert(json[r'retention'] != null, 'Required key "Memo[retention]" has a null value in JSON.');
        assert(json.containsKey(r'content_hash'), 'Required key "Memo[content_hash]" is missing from JSON.');
        assert(json[r'content_hash'] != null, 'Required key "Memo[content_hash]" has a null value in JSON.');
        assert(json.containsKey(r'byte_size'), 'Required key "Memo[byte_size]" is missing from JSON.');
        assert(json[r'byte_size'] != null, 'Required key "Memo[byte_size]" has a null value in JSON.');
        assert(json.containsKey(r'captured_at'), 'Required key "Memo[captured_at]" is missing from JSON.');
        assert(json[r'captured_at'] != null, 'Required key "Memo[captured_at]" has a null value in JSON.');
        assert(json.containsKey(r'recorded_at'), 'Required key "Memo[recorded_at]" is missing from JSON.');
        assert(json.containsKey(r'audio_pruned'), 'Required key "Memo[audio_pruned]" is missing from JSON.');
        assert(json[r'audio_pruned'] != null, 'Required key "Memo[audio_pruned]" has a null value in JSON.');
        assert(json.containsKey(r'retention_status'), 'Required key "Memo[retention_status]" is missing from JSON.');
        assert(json[r'retention_status'] != null, 'Required key "Memo[retention_status]" has a null value in JSON.');
        assert(json.containsKey(r'prunes_at'), 'Required key "Memo[prunes_at]" is missing from JSON.');
        assert(json.containsKey(r'audio_pruned_at'), 'Required key "Memo[audio_pruned_at]" is missing from JSON.');
        assert(json.containsKey(r'duration_ms'), 'Required key "Memo[duration_ms]" is missing from JSON.');
        assert(json.containsKey(r'codec'), 'Required key "Memo[codec]" is missing from JSON.');
        assert(json.containsKey(r'sample_rate_hz'), 'Required key "Memo[sample_rate_hz]" is missing from JSON.');
        return true;
      }());

      return Memo(
        id: mapValueOfType<String>(json, r'id')!,
        state: mapValueOfType<String>(json, r'state')!,
        retention: MemoRetentionEnum.fromJson(json[r'retention'])!,
        contentHash: mapValueOfType<String>(json, r'content_hash')!,
        byteSize: mapValueOfType<int>(json, r'byte_size')!,
        capturedAt: mapDateTime(json, r'captured_at', r'')!,
        recordedAt: mapDateTime(json, r'recorded_at', r''),
        audioPruned: mapValueOfType<bool>(json, r'audio_pruned')!,
        retentionStatus: mapValueOfType<String>(json, r'retention_status')!,
        prunesAt: mapDateTime(json, r'prunes_at', r''),
        audioPrunedAt: mapDateTime(json, r'audio_pruned_at', r''),
        durationMs: mapValueOfType<int>(json, r'duration_ms'),
        codec: mapValueOfType<String>(json, r'codec'),
        sampleRateHz: mapValueOfType<int>(json, r'sample_rate_hz'),
        originalFilename: mapValueOfType<String>(json, r'original_filename'),
      );
    }
    return null;
  }

  static List<Memo> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Memo>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Memo.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Memo> mapFromJson(dynamic json) {
    final map = <String, Memo>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Memo.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Memo-objects as value to a dart map
  static Map<String, List<Memo>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Memo>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Memo.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'id',
    'state',
    'retention',
    'content_hash',
    'byte_size',
    'captured_at',
    'recorded_at',
    'audio_pruned',
    'retention_status',
    'prunes_at',
    'audio_pruned_at',
    'duration_ms',
    'codec',
    'sample_rate_hz',
  };
}


class MemoRetentionEnum {
  /// Instantiate a new enum with the provided [value].
  const MemoRetentionEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const discardNow = MemoRetentionEnum._(r'discard_now');
  static const days30 = MemoRetentionEnum._(r'days_30');
  static const forever = MemoRetentionEnum._(r'forever');

  /// List of all possible values in this [enum][MemoRetentionEnum].
  static const values = <MemoRetentionEnum>[
    discardNow,
    days30,
    forever,
  ];

  static MemoRetentionEnum? fromJson(dynamic value) => MemoRetentionEnumTypeTransformer().decode(value);

  static List<MemoRetentionEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <MemoRetentionEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = MemoRetentionEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [MemoRetentionEnum] to String,
/// and [decode] dynamic data back to [MemoRetentionEnum].
class MemoRetentionEnumTypeTransformer {
  factory MemoRetentionEnumTypeTransformer() => _instance ??= const MemoRetentionEnumTypeTransformer._();

  const MemoRetentionEnumTypeTransformer._();

  String encode(MemoRetentionEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a MemoRetentionEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  MemoRetentionEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'discard_now': return MemoRetentionEnum.discardNow;
        case r'days_30': return MemoRetentionEnum.days30;
        case r'forever': return MemoRetentionEnum.forever;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [MemoRetentionEnumTypeTransformer] instance.
  static MemoRetentionEnumTypeTransformer? _instance;
}


