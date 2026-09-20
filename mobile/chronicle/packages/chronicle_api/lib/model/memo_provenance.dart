//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class MemoProvenance {
  /// Returns a new [MemoProvenance] instance.
  MemoProvenance({
    required this.revisionSeq,
    required this.revisionId,
    required this.memoId,
    required this.capturedAt,
    required this.durationMs,
    this.durationSource,
    required this.audioReadable,
    required this.retentionStatus,
    required this.prunesAt,
    required this.audioPrunedAt,
    required this.transcript,
  });

  /// Which revision of the note this memo's text became. Zips against `listNoteRevisions`, whose `seq` is the same number. 
  int revisionSeq;

  String revisionId;

  /// Pass it to `getMemoTranscript` or `getMemoAudio`.
  String memoId;

  /// When it was recorded. Immutable — `CH002` refuses an UPDATE that moves it — which is also why it is what the audio stream's `Last-Modified` is built from. 
  DateTime capturedAt;

  /// How long the recording is. Null for a memo with neither a header duration nor a transcript — the pre-transcription window, and honest rather than a zero. 
  int? durationMs;

  /// WHICH COLUMN ANSWERED, because two hold a duration and they are measured differently: `memos.duration_ms` is Ogg granule arithmetic, exact, and populated for Ogg Opus only, while `transcripts.audio_duration_ms` is measured off the normalised 16 kHz mono WAV and is populated for everything that transcribes. Every memo in the live corpus arrives m4a with a NULL header duration, so today this says `transcript` — and reading the header column alone would render a blank where a number should be, for the whole corpus, with no error anywhere.  Stating the source rather than quietly preferring one is what keeps CHRN-85's question open: whichever of its three shapes wins, this field collapses to a single value and the contract does not change. Absent exactly when `duration_ms` is null. 
  MemoProvenanceDurationSourceEnum? durationSource;

  /// **A permission, and it says nothing about the disk.** True when this caller may ask `getMemoAudio` for the bytes: the memo's author, or the owner. Whether the bytes still EXIST is `retention_status`'s job — `pruned` says they went by policy, and CHRN-23's `missing` is a disk fact no query behind this payload performs. A field joining the two would answer `true` for a memo whose file has gone astray, which is the one state this contract must not describe optimistically. 
  bool audioReadable;

  /// What will happen to this memo's audio, from `store.RetentionStatus` — the same clause the pruner sweeps with, which is what makes the date rendered here the date the job will use rather than a second calculation that can drift from it. 
  MemoProvenanceRetentionStatusEnum retentionStatus;

  /// When the audio is due to go. **Null on every status but `scheduled`**, and never computable from `captured_at` by a client: `awaiting_transcript` prunes WHEN TRANSCRIBED and has no date at all, and rendering `captured_at + 30 days` in its place produces exactly the label CHRN-22 §3 forbids — one that passes while nothing happens. 
  DateTime? prunesAt;

  /// When the audio was deleted, and null until it is. Set exactly when `retention_status` is `pruned`. **This and `transcript.present` are what *transcript kept, audio pruned <date>* is rendered from** — never the audio operation's refusal, which carries only a `code` and a `message` and which an `<audio src>` element never sees at all. 
  DateTime? audioPrunedAt;

  ProvenanceTranscript transcript;

  @override
  bool operator ==(Object other) => identical(this, other) || other is MemoProvenance &&
    other.revisionSeq == revisionSeq &&
    other.revisionId == revisionId &&
    other.memoId == memoId &&
    other.capturedAt == capturedAt &&
    other.durationMs == durationMs &&
    other.durationSource == durationSource &&
    other.audioReadable == audioReadable &&
    other.retentionStatus == retentionStatus &&
    other.prunesAt == prunesAt &&
    other.audioPrunedAt == audioPrunedAt &&
    other.transcript == transcript;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (revisionSeq.hashCode) +
    (revisionId.hashCode) +
    (memoId.hashCode) +
    (capturedAt.hashCode) +
    (durationMs == null ? 0 : durationMs!.hashCode) +
    (durationSource == null ? 0 : durationSource!.hashCode) +
    (audioReadable.hashCode) +
    (retentionStatus.hashCode) +
    (prunesAt == null ? 0 : prunesAt!.hashCode) +
    (audioPrunedAt == null ? 0 : audioPrunedAt!.hashCode) +
    (transcript.hashCode);

  @override
  String toString() => 'MemoProvenance[revisionSeq=$revisionSeq, revisionId=$revisionId, memoId=$memoId, capturedAt=$capturedAt, durationMs=$durationMs, durationSource=$durationSource, audioReadable=$audioReadable, retentionStatus=$retentionStatus, prunesAt=$prunesAt, audioPrunedAt=$audioPrunedAt, transcript=$transcript]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'revision_seq'] = this.revisionSeq;
      json[r'revision_id'] = this.revisionId;
      json[r'memo_id'] = this.memoId;
      json[r'captured_at'] = this.capturedAt.toUtc().toIso8601String();
    if (this.durationMs != null) {
      json[r'duration_ms'] = this.durationMs;
    } else {
      json[r'duration_ms'] = null;
    }
    if (this.durationSource != null) {
      json[r'duration_source'] = this.durationSource;
    } else {
      json[r'duration_source'] = null;
    }
      json[r'audio_readable'] = this.audioReadable;
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
      json[r'transcript'] = this.transcript;
    return json;
  }

  /// Returns a new [MemoProvenance] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static MemoProvenance? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'revision_seq'), 'Required key "MemoProvenance[revision_seq]" is missing from JSON.');
        assert(json[r'revision_seq'] != null, 'Required key "MemoProvenance[revision_seq]" has a null value in JSON.');
        assert(json.containsKey(r'revision_id'), 'Required key "MemoProvenance[revision_id]" is missing from JSON.');
        assert(json[r'revision_id'] != null, 'Required key "MemoProvenance[revision_id]" has a null value in JSON.');
        assert(json.containsKey(r'memo_id'), 'Required key "MemoProvenance[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "MemoProvenance[memo_id]" has a null value in JSON.');
        assert(json.containsKey(r'captured_at'), 'Required key "MemoProvenance[captured_at]" is missing from JSON.');
        assert(json[r'captured_at'] != null, 'Required key "MemoProvenance[captured_at]" has a null value in JSON.');
        assert(json.containsKey(r'duration_ms'), 'Required key "MemoProvenance[duration_ms]" is missing from JSON.');
        assert(json.containsKey(r'audio_readable'), 'Required key "MemoProvenance[audio_readable]" is missing from JSON.');
        assert(json[r'audio_readable'] != null, 'Required key "MemoProvenance[audio_readable]" has a null value in JSON.');
        assert(json.containsKey(r'retention_status'), 'Required key "MemoProvenance[retention_status]" is missing from JSON.');
        assert(json[r'retention_status'] != null, 'Required key "MemoProvenance[retention_status]" has a null value in JSON.');
        assert(json.containsKey(r'prunes_at'), 'Required key "MemoProvenance[prunes_at]" is missing from JSON.');
        assert(json.containsKey(r'audio_pruned_at'), 'Required key "MemoProvenance[audio_pruned_at]" is missing from JSON.');
        assert(json.containsKey(r'transcript'), 'Required key "MemoProvenance[transcript]" is missing from JSON.');
        assert(json[r'transcript'] != null, 'Required key "MemoProvenance[transcript]" has a null value in JSON.');
        return true;
      }());

      return MemoProvenance(
        revisionSeq: mapValueOfType<int>(json, r'revision_seq')!,
        revisionId: mapValueOfType<String>(json, r'revision_id')!,
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        capturedAt: mapDateTime(json, r'captured_at', r'')!,
        durationMs: mapValueOfType<int>(json, r'duration_ms'),
        durationSource: MemoProvenanceDurationSourceEnum.fromJson(json[r'duration_source']),
        audioReadable: mapValueOfType<bool>(json, r'audio_readable')!,
        retentionStatus: MemoProvenanceRetentionStatusEnum.fromJson(json[r'retention_status'])!,
        prunesAt: mapDateTime(json, r'prunes_at', r''),
        audioPrunedAt: mapDateTime(json, r'audio_pruned_at', r''),
        transcript: ProvenanceTranscript.fromJson(json[r'transcript'])!,
      );
    }
    return null;
  }

  static List<MemoProvenance> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <MemoProvenance>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = MemoProvenance.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, MemoProvenance> mapFromJson(dynamic json) {
    final map = <String, MemoProvenance>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = MemoProvenance.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of MemoProvenance-objects as value to a dart map
  static Map<String, List<MemoProvenance>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<MemoProvenance>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = MemoProvenance.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'revision_seq',
    'revision_id',
    'memo_id',
    'captured_at',
    'duration_ms',
    'audio_readable',
    'retention_status',
    'prunes_at',
    'audio_pruned_at',
    'transcript',
  };
}

/// WHICH COLUMN ANSWERED, because two hold a duration and they are measured differently: `memos.duration_ms` is Ogg granule arithmetic, exact, and populated for Ogg Opus only, while `transcripts.audio_duration_ms` is measured off the normalised 16 kHz mono WAV and is populated for everything that transcribes. Every memo in the live corpus arrives m4a with a NULL header duration, so today this says `transcript` — and reading the header column alone would render a blank where a number should be, for the whole corpus, with no error anywhere.  Stating the source rather than quietly preferring one is what keeps CHRN-85's question open: whichever of its three shapes wins, this field collapses to a single value and the contract does not change. Absent exactly when `duration_ms` is null. 
class MemoProvenanceDurationSourceEnum {
  /// Instantiate a new enum with the provided [value].
  const MemoProvenanceDurationSourceEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const memoHeader = MemoProvenanceDurationSourceEnum._(r'memo_header');
  static const transcript = MemoProvenanceDurationSourceEnum._(r'transcript');

  /// List of all possible values in this [enum][MemoProvenanceDurationSourceEnum].
  static const values = <MemoProvenanceDurationSourceEnum>[
    memoHeader,
    transcript,
  ];

  static MemoProvenanceDurationSourceEnum? fromJson(dynamic value) => MemoProvenanceDurationSourceEnumTypeTransformer().decode(value);

  static List<MemoProvenanceDurationSourceEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <MemoProvenanceDurationSourceEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = MemoProvenanceDurationSourceEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [MemoProvenanceDurationSourceEnum] to String,
/// and [decode] dynamic data back to [MemoProvenanceDurationSourceEnum].
class MemoProvenanceDurationSourceEnumTypeTransformer {
  factory MemoProvenanceDurationSourceEnumTypeTransformer() => _instance ??= const MemoProvenanceDurationSourceEnumTypeTransformer._();

  const MemoProvenanceDurationSourceEnumTypeTransformer._();

  String encode(MemoProvenanceDurationSourceEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a MemoProvenanceDurationSourceEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  MemoProvenanceDurationSourceEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'memo_header': return MemoProvenanceDurationSourceEnum.memoHeader;
        case r'transcript': return MemoProvenanceDurationSourceEnum.transcript;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [MemoProvenanceDurationSourceEnumTypeTransformer] instance.
  static MemoProvenanceDurationSourceEnumTypeTransformer? _instance;
}


/// What will happen to this memo's audio, from `store.RetentionStatus` — the same clause the pruner sweeps with, which is what makes the date rendered here the date the job will use rather than a second calculation that can drift from it. 
class MemoProvenanceRetentionStatusEnum {
  /// Instantiate a new enum with the provided [value].
  const MemoProvenanceRetentionStatusEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const pruned = MemoProvenanceRetentionStatusEnum._(r'pruned');
  static const pinned = MemoProvenanceRetentionStatusEnum._(r'pinned');
  static const awaitingTranscript = MemoProvenanceRetentionStatusEnum._(r'awaiting_transcript');
  static const discardPending = MemoProvenanceRetentionStatusEnum._(r'discard_pending');
  static const scheduled = MemoProvenanceRetentionStatusEnum._(r'scheduled');

  /// List of all possible values in this [enum][MemoProvenanceRetentionStatusEnum].
  static const values = <MemoProvenanceRetentionStatusEnum>[
    pruned,
    pinned,
    awaitingTranscript,
    discardPending,
    scheduled,
  ];

  static MemoProvenanceRetentionStatusEnum? fromJson(dynamic value) => MemoProvenanceRetentionStatusEnumTypeTransformer().decode(value);

  static List<MemoProvenanceRetentionStatusEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <MemoProvenanceRetentionStatusEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = MemoProvenanceRetentionStatusEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [MemoProvenanceRetentionStatusEnum] to String,
/// and [decode] dynamic data back to [MemoProvenanceRetentionStatusEnum].
class MemoProvenanceRetentionStatusEnumTypeTransformer {
  factory MemoProvenanceRetentionStatusEnumTypeTransformer() => _instance ??= const MemoProvenanceRetentionStatusEnumTypeTransformer._();

  const MemoProvenanceRetentionStatusEnumTypeTransformer._();

  String encode(MemoProvenanceRetentionStatusEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a MemoProvenanceRetentionStatusEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  MemoProvenanceRetentionStatusEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'pruned': return MemoProvenanceRetentionStatusEnum.pruned;
        case r'pinned': return MemoProvenanceRetentionStatusEnum.pinned;
        case r'awaiting_transcript': return MemoProvenanceRetentionStatusEnum.awaitingTranscript;
        case r'discard_pending': return MemoProvenanceRetentionStatusEnum.discardPending;
        case r'scheduled': return MemoProvenanceRetentionStatusEnum.scheduled;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [MemoProvenanceRetentionStatusEnumTypeTransformer] instance.
  static MemoProvenanceRetentionStatusEnumTypeTransformer? _instance;
}


