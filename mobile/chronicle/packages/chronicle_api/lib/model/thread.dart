//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class Thread {
  /// Returns a new [Thread] instance.
  Thread({
    required this.discussion,
    this.turns = const [],
    this.participants = const [],
    this.unread,
  });

  Discussion discussion;

  /// In `seq` order, which is the only order.
  List<Turn> turns;

  /// Everybody ever on the thread, oldest first; removed ones carry their removal.
  List<Participant> participants;

  /// The caller's unread turns, computed by the store. Absent when the caller is not a participant, and absent for an agent — an agent has no unread. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  int? unread;

  @override
  bool operator ==(Object other) => identical(this, other) || other is Thread &&
    other.discussion == discussion &&
    _deepEquality.equals(other.turns, turns) &&
    _deepEquality.equals(other.participants, participants) &&
    other.unread == unread;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (discussion.hashCode) +
    (turns.hashCode) +
    (participants.hashCode) +
    (unread == null ? 0 : unread!.hashCode);

  @override
  String toString() => 'Thread[discussion=$discussion, turns=$turns, participants=$participants, unread=$unread]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'discussion'] = this.discussion;
      json[r'turns'] = this.turns;
      json[r'participants'] = this.participants;
    if (this.unread != null) {
      json[r'unread'] = this.unread;
    } else {
      json[r'unread'] = null;
    }
    return json;
  }

  /// Returns a new [Thread] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static Thread? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'discussion'), 'Required key "Thread[discussion]" is missing from JSON.');
        assert(json[r'discussion'] != null, 'Required key "Thread[discussion]" has a null value in JSON.');
        assert(json.containsKey(r'turns'), 'Required key "Thread[turns]" is missing from JSON.');
        assert(json[r'turns'] != null, 'Required key "Thread[turns]" has a null value in JSON.');
        assert(json.containsKey(r'participants'), 'Required key "Thread[participants]" is missing from JSON.');
        assert(json[r'participants'] != null, 'Required key "Thread[participants]" has a null value in JSON.');
        return true;
      }());

      return Thread(
        discussion: Discussion.fromJson(json[r'discussion'])!,
        turns: Turn.listFromJson(json[r'turns']),
        participants: Participant.listFromJson(json[r'participants']),
        unread: mapValueOfType<int>(json, r'unread'),
      );
    }
    return null;
  }

  static List<Thread> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <Thread>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = Thread.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, Thread> mapFromJson(dynamic json) {
    final map = <String, Thread>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = Thread.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of Thread-objects as value to a dart map
  static Map<String, List<Thread>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<Thread>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = Thread.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'discussion',
    'turns',
    'participants',
  };
}

