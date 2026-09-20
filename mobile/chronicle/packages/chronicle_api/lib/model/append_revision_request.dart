//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;

class AppendRevisionRequest {
  /// Returns a new [AppendRevisionRequest] instance.
  AppendRevisionRequest({
    this.title,
    required this.body,
  });

  /// Omitted keeps the current title.
  ///
  /// Please note: This property should have been non-nullable! Since the specification file
  /// does not include a default value (using the "default:" property), however, the generated
  /// source code must fall back to having a nullable type.
  /// Consider adding a "default:" property in the specification file to hide this note.
  ///
  String? title;

  /// Markdown, stored raw. The whole new body, not a diff.
  String body;

  @override
  bool operator ==(Object other) => identical(this, other) || other is AppendRevisionRequest &&
    other.title == title &&
    other.body == body;

  @override
  int get hashCode =>
    // ignore: unnecessary_parenthesis
    (title == null ? 0 : title!.hashCode) +
    (body.hashCode);

  @override
  String toString() => 'AppendRevisionRequest[title=$title, body=$body]';

  Map<String, dynamic> toJson() {
    final json = <String, dynamic>{};
    if (this.title != null) {
      json[r'title'] = this.title;
    } else {
      json[r'title'] = null;
    }
      json[r'body'] = this.body;
    return json;
  }

  /// Returns a new [AppendRevisionRequest] instance and imports its values from
  /// [value] if it's a [Map], null otherwise.
  // ignore: prefer_constructors_over_static_methods
  static AppendRevisionRequest? fromJson(dynamic value) {
    if (value is Map) {
      final json = value.cast<String, dynamic>();

      // Ensure that the map contains the required keys.
      // Note 1: the values aren't checked for validity beyond being non-null.
      // Note 2: this code is stripped in release mode!
      assert(() {
        assert(json.containsKey(r'body'), 'Required key "AppendRevisionRequest[body]" is missing from JSON.');
        assert(json[r'body'] != null, 'Required key "AppendRevisionRequest[body]" has a null value in JSON.');
        return true;
      }());

      return AppendRevisionRequest(
        title: mapValueOfType<String>(json, r'title'),
        body: mapValueOfType<String>(json, r'body')!,
      );
    }
    return null;
  }

  static List<AppendRevisionRequest> listFromJson(dynamic json, {bool growable = false,}) {
    final result = <AppendRevisionRequest>[];
    if (json is List && json.isNotEmpty) {
      for (final row in json) {
        final value = AppendRevisionRequest.fromJson(row);
        if (value != null) {
          result.add(value);
        }
      }
    }
    return result.toList(growable: growable);
  }

  static Map<String, AppendRevisionRequest> mapFromJson(dynamic json) {
    final map = <String, AppendRevisionRequest>{};
    if (json is Map && json.isNotEmpty) {
      json = json.cast<String, dynamic>(); // ignore: parameter_assignments
      for (final entry in json.entries) {
        final value = AppendRevisionRequest.fromJson(entry.value);
        if (value != null) {
          map[entry.key] = value;
        }
      }
    }
    return map;
  }

  // maps a json object with a list of AppendRevisionRequest-objects as value to a dart map
  static Map<String, List<AppendRevisionRequest>> mapListFromJson(dynamic json, {bool growable = false,}) {
    final map = <String, List<AppendRevisionRequest>>{};
    if (json is Map && json.isNotEmpty) {
      // ignore: parameter_assignments
      json = json.cast<String, dynamic>();
      for (final entry in json.entries) {
        map[entry.key] = AppendRevisionRequest.listFromJson(entry.value, growable: growable,);
      }
    }
    return map;
  }

  /// The list of required keys that must be present in a JSON.
  static const requiredKeys = <String>{
    'body',
  };
}

