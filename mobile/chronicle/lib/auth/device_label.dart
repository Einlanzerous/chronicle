/// What this device calls itself in the session list its holder reads.
///
/// `device_label` is optional on `POST /auth/session`, but a session list of
/// four rows all reading "Android" is a list nobody can revoke safely — and
/// revoking the wrong device is how a person locks themselves out of the app
/// they are holding. CHRN-106's board calls these *devices*, never *phones*, so
/// the label says what the thing is rather than what kind of thing it is.
library;

import 'package:device_info_plus/device_info_plus.dart';

/// The device's model name, e.g. `Pixel 8`. Falls back to something true rather
/// than something invented when the platform will not say.
Future<String> deviceLabel({DeviceInfoPlugin? plugin}) async {
  try {
    final info = await (plugin ?? DeviceInfoPlugin()).androidInfo;
    final model = info.model.trim();
    if (model.isNotEmpty) return model;
    final device = info.device.trim();
    if (device.isNotEmpty) return device;
  } catch (_) {
    // A platform channel that is not there (a unit test, an unsupported
    // platform) is not a reason to refuse to sign in. The label is a
    // convenience; the session is not.
  }
  return 'Android device';
}
