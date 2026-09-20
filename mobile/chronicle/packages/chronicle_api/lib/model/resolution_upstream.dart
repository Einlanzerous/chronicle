//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class ResolutionUpstream {
  /// Returns a new [ResolutionUpstream] instance.
  ResolutionUpstream({
    this.key,
    this.outcome,
    this.displayName,
    this.title,
    this.url,
    this.recoverable,
  });

  /// The key AS ANSWERED, which is not always the key as written: Switchyard follows ticket aliases after a move, so `IDEA-21` can answer with key `CHRN-7`, and a `CHR-311` written by hand answers `CHR-0311`. On a `broken`, the key as written. Absent for Amber, which has no key. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? key;

  /// The UPSTREAM'S OWN vocabulary member, where it publishes one. Switchyard: the status category of a live ticket, and nothing on a missing one — it has no member meaning \"deleted\", and Chronicle does not mint one in somebody else's namespace to fill a field. Amber: `held`, `not_captured`, `prompt_only`, `not_in_capture`, `block_out_of_range`, `ambiguous`, or `malformed`. Chronicle's own: `deleted` on a soft-deleted note, `open` or `resolved` on a discussion, and nothing on a live note, which has no state to report. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? outcome;

  /// The status as a person reads it. Switchyard only.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? displayName;

  /// The ticket's, the note's or the thread's title. Amber's card has no title to show, and a deleted note's is withheld along with its body. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? title;

  /// The outbound arrow's target. Absent where one would not land — a deleted ticket answers 404, Amber serves no HTML — and absent for Chronicle's own references, which the client holding them already knows how to open. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? url;

  /// Amber's, relayed verbatim, and MEANINGFUL ONLY ON `broken`: `not_in_capture` and `ambiguous` are recoverable; `held`, the only resolved outcome, is a constant false. Present whenever Amber answered and absent for every other system. 
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  bool? recoverable;

  @override
  bool operator ==(Object other) => identical(this, other) || other is ResolutionUpstream &&
    other.key == key &&
    other.outcome == outcome &&
    other.displayName == displayName &&
    other.title == title &&
    other.url == url &&
    other.recoverable == recoverable;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (key == null ? 0 : key!.hashCode) +
    (outcome == null ? 0 : outcome!.hashCode) +
    (displayName == null ? 0 : displayName!.hashCode) +
    (title == null ? 0 : title!.hashCode) +
    (url == null ? 0 : url!.hashCode) +
    (recoverable == null ? 0 : recoverable!.hashCode);

  @override
  String toString() => 'ResolutionUpstream[key=$key, outcome=$outcome, displayName=$displayName, title=$title, url=$url, recoverable=$recoverable]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.key != null) {
      json[r'key'] = this.key;
    } else {
      json[r'key'] = null;
    }
    if (this.outcome != null) {
      json[r'outcome'] = this.outcome;
    } else {
      json[r'outcome'] = null;
    }
    if (this.displayName != null) {
      json[r'display_name'] = this.displayName;
    } else {
      json[r'display_name'] = null;
    }
    if (this.title != null) {
      json[r'title'] = this.title;
    } else {
      json[r'title'] = null;
    }
    if (this.url != null) {
      json[r'url'] = this.url;
    } else {
      json[r'url'] = null;
    }
    if (this.recoverable != null) {
      json[r'recoverable'] = this.recoverable;
    } else {
      json[r'recoverable'] = null;
    }
    return json;
  }

  /// Returns a new [ResolutionUpstream] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static ResolutionUpstream? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        return true;
      }());

      return ResolutionUpstream(
        key: mapValueOfType<String>(json, r'key'),
        outcome: mapValueOfType<String>(json, r'outcome'),
        displayName: mapValueOfType<String>(json, r'display_name'),
        title: mapValueOfType<String>(json, r'title'),
        url: mapValueOfType<String>(json, r'url'),
        recoverable: mapValueOfType<bool>(json, r'recoverable'),
      );
    }
    return null;
  }

  static List<ResolutionUpstream> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <ResolutionUpstream>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = ResolutionUpstream.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, ResolutionUpstream> mapFromJson(dynamic json) {
    final map = <String, ResolutionUpstream>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = ResolutionUpstream.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of ResolutionUpstream-objects as value to a dart map
  static Map<String, List<ResolutionUpstream>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<ResolutionUpstream>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = ResolutionUpstream.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
  };
}

