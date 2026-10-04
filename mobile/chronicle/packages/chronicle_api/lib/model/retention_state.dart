//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class RetentionState {
  /// Returns a new [RetentionState] instance.
  RetentionState({
    required this.memoId,
    required this.retention,
    required this.retentionStatus,
    required this.prunesAt,
  });

  String memoId;

  RetentionStateRetentionEnum retention;

  /// One of `MemoProvenance.retention_status`'s five values, from the same `store.RetentionStatus`. Declared as a string and not as a second copy of that enum, on `Memo.retention_status`'s precedent: one list to keep in step, in the one place a client renders from. 
  String retentionStatus;

  /// `MemoProvenance.prunes_at`: null on every status but `scheduled`.
  DateTime? prunesAt;

  @override
  bool operator ==(Object other) => identical(this, other) || other is RetentionState &&
    other.memoId == memoId &&
    other.retention == retention &&
    other.retentionStatus == retentionStatus &&
    other.prunesAt == prunesAt;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (memoId.hashCode) +
    (retention.hashCode) +
    (retentionStatus.hashCode) +
    (prunesAt == null ? 0 : prunesAt!.hashCode);

  @override
  String toString() => 'RetentionState[memoId=$memoId, retention=$retention, retentionStatus=$retentionStatus, prunesAt=$prunesAt]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'memo_id'] = this.memoId;
      json[r'retention'] = this.retention;
      json[r'retention_status'] = this.retentionStatus;
    if (this.prunesAt != null) {
      json[r'prunes_at'] = this.prunesAt!.toUtc().toIso8601String();
    } else {
      json[r'prunes_at'] = null;
    }
    return json;
  }

  /// Returns a new [RetentionState] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static RetentionState? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'memo_id'), 'Required key "RetentionState[memo_id]" is missing from JSON.');
        assert(json[r'memo_id'] != null, 'Required key "RetentionState[memo_id]" has a null value in JSON.');
        assert(json.containsKey(r'retention'), 'Required key "RetentionState[retention]" is missing from JSON.');
        assert(json[r'retention'] != null, 'Required key "RetentionState[retention]" has a null value in JSON.');
        assert(json.containsKey(r'retention_status'), 'Required key "RetentionState[retention_status]" is missing from JSON.');
        assert(json[r'retention_status'] != null, 'Required key "RetentionState[retention_status]" has a null value in JSON.');
        assert(json.containsKey(r'prunes_at'), 'Required key "RetentionState[prunes_at]" is missing from JSON.');
        return true;
      }());

      return RetentionState(
        memoId: mapValueOfType<String>(json, r'memo_id')!,
        retention: RetentionStateRetentionEnum.fromJson(json[r'retention'])!,
        retentionStatus: mapValueOfType<String>(json, r'retention_status')!,
        prunesAt: mapDateTime(json, r'prunes_at', r''),
      );
    }
    return null;
  }

  static List<RetentionState> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RetentionState>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RetentionState.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, RetentionState> mapFromJson(dynamic json) {
    final map = <String, RetentionState>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = RetentionState.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of RetentionState-objects as value to a dart map
  static Map<String, List<RetentionState>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<RetentionState>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = RetentionState.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'memo_id',
    'retention',
    'retention_status',
    'prunes_at',
  };
}


class RetentionStateRetentionEnum {
  /// Instantiate a new enum with the provided [value].
  const RetentionStateRetentionEnum._(this.value);

  /// The underlying value of this enum member.
  final String value;

  @override
  String toString() => value;

  String toJson() => value;

  static const discardNow = RetentionStateRetentionEnum._(r'discard_now');
  static const days30 = RetentionStateRetentionEnum._(r'days_30');
  static const forever = RetentionStateRetentionEnum._(r'forever');

  /// List of all possible values in this [enum][RetentionStateRetentionEnum].
  static const values = <RetentionStateRetentionEnum>[
    discardNow,
    days30,
    forever,
  ];

  static RetentionStateRetentionEnum? fromJson(dynamic value) => RetentionStateRetentionEnumTypeTransformer().decode(value);

  static List<RetentionStateRetentionEnum> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <RetentionStateRetentionEnum>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = RetentionStateRetentionEnum.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }
}

/// Transformation class that can [encode] an instance of [RetentionStateRetentionEnum] to String,
/// and [decode] dynamic data back to [RetentionStateRetentionEnum].
class RetentionStateRetentionEnumTypeTransformer {
  factory RetentionStateRetentionEnumTypeTransformer() => _instance ??= const RetentionStateRetentionEnumTypeTransformer._();

  const RetentionStateRetentionEnumTypeTransformer._();

  String encode(RetentionStateRetentionEnum data) => data.value;

  /// Decodes a [dynamic value][data] to a RetentionStateRetentionEnum.
  ///
  /// If [allowNull] is true and the [dynamic value][data] cannot be decoded successfully,
  /// then null is returned. However, if [allowNull] is false and the [dynamic value][data]
  /// cannot be decoded successfully, then an [UnimplementedError] is thrown.
  ///
  /// The [allowNull] is very handy when an API changes and a new enum value is added or removed,
  /// and users are still using an old app with the old code.
  RetentionStateRetentionEnum? decode(dynamic data, {bool allowNull = true}) {
    if (data != null) {
      switch (data) {
        case r'discard_now': return RetentionStateRetentionEnum.discardNow;
        case r'days_30': return RetentionStateRetentionEnum.days30;
        case r'forever': return RetentionStateRetentionEnum.forever;
        default:
          if (!allowNull) {
            throw ArgumentError('Unknown enum value to decode: $data');
          }
      }
    }
    return null;
  }

  /// Singleton [RetentionStateRetentionEnumTypeTransformer] instance.
  static RetentionStateRetentionEnumTypeTransformer? _instance;
}


