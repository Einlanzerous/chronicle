//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class DiscussionResolution {
  /// Returns a new [DiscussionResolution] instance.
  DiscussionResolution({
    required this.discussion,
    this.note,
  });

  Discussion discussion;

  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  ResolvedNote? note;

  @override
  bool operator ==(Object other) => identical(this, other) || other is DiscussionResolution &&
    other.discussion == discussion &&
    other.note == note;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (discussion.hashCode) +
    (note == null ? 0 : note!.hashCode);

  @override
  String toString() => 'DiscussionResolution[discussion=$discussion, note=$note]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
      json[r'discussion'] = this.discussion;
    if (this.note != null) {
      json[r'note'] = this.note;
    } else {
      json[r'note'] = null;
    }
    return json;
  }

  /// Returns a new [DiscussionResolution] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static DiscussionResolution? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'discussion'), 'Required key "DiscussionResolution[discussion]" is missing from JSON.');
        assert(json[r'discussion'] != null, 'Required key "DiscussionResolution[discussion]" has a null value in JSON.');
        return true;
      }());

      return DiscussionResolution(
        discussion: Discussion.fromJson(json[r'discussion'])!,
        note: ResolvedNote.fromJson(json[r'note']),
      );
    }
    return null;
  }

  static List<DiscussionResolution> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <DiscussionResolution>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = DiscussionResolution.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, DiscussionResolution> mapFromJson(dynamic json) {
    final map = <String, DiscussionResolution>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = DiscussionResolution.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of DiscussionResolution-objects as value to a dart map
  static Map<String, List<DiscussionResolution>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<DiscussionResolution>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = DiscussionResolution.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'discussion',
  };
}

