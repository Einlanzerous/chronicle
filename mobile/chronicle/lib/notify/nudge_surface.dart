/// Where the evening nudge (CHRN-64) meets Android. `nudge.dart` decides;
/// this posts, withdraws, and hears the tap.
///
/// `flutter_local_notifications` rather than a channel on `MainActivity`,
/// because the pass that posts runs in WorkManager's headless engine
/// (`queue/background.dart`), where `MainActivity`'s channels do not exist and
/// a registered plugin does.
library;

import 'package:flutter_local_notifications/flutter_local_notifications.dart';

/// One fixed id: a second post replaces the first rather than stacking beside
/// it, which is half of "exactly one notification".
const nudgeNotificationId = 6401;
const nudgePayload = 'triage';

const _channelId = 'triage_nudge';

String nudgeTitle({required int count, required bool atLeast}) {
  if (atLeast) return '$count+ memos waiting for triage';
  return count == 1 ? '1 memo waiting for triage' : '$count memos waiting for triage';
}

const nudgeBody = 'Transcribed and routed. Tap to go through them.';

abstract class NudgeSurface {
  /// Posts the nudge, alerting. False when the system will not show it.
  Future<bool> post({required int count, required bool atLeast});

  /// Rewrites the number on a notification that is already showing, silently.
  Future<void> update({required int count, required bool atLeast});

  Future<void> cancel();

  /// Whether the nudge is in the shade right now.
  Future<bool> showing();
}

class LocalNudgeSurface implements NudgeSurface {
  LocalNudgeSurface([FlutterLocalNotificationsPlugin? plugin])
      : _plugin = plugin ?? FlutterLocalNotificationsPlugin();

  final FlutterLocalNotificationsPlugin _plugin;

  AndroidFlutterLocalNotificationsPlugin? get _android =>
      _plugin.resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>();

  /// Must run in every isolate that uses the surface. [onTap] is the foreground
  /// isolate's: it fires when the nudge is tapped while the process is alive.
  Future<void> init({void Function()? onTap}) async {
    await _plugin.initialize(
      settings: const InitializationSettings(
        android: AndroidInitializationSettings('ic_stat_chronicle'),
      ),
      onDidReceiveNotificationResponse: (response) {
        if (response.payload == nudgePayload) onTap?.call();
      },
    );
  }

  /// Whether a tap on the nudge is what started this process -- the cold case
  /// [init]'s callback cannot cover, because nothing was listening yet.
  Future<bool> launchedFromNudge() async {
    final details = await _plugin.getNotificationAppLaunchDetails();
    return details != null &&
        details.didNotificationLaunchApp &&
        details.notificationResponse?.payload == nudgePayload;
  }

  NotificationDetails _details({required bool quiet}) => NotificationDetails(
        android: AndroidNotificationDetails(
          _channelId,
          'Triage',
          channelDescription: 'One nudge in the evening when memos are waiting to be triaged.',
          importance: Importance.defaultImportance,
          priority: Priority.defaultPriority,
          silent: quiet,
          onlyAlertOnce: true,
          category: AndroidNotificationCategory.reminder,
        ),
      );

  @override
  Future<bool> post({required int count, required bool atLeast}) async {
    if (!(await _android?.areNotificationsEnabled() ?? false)) return false;
    // Withdrawn first so that yesterday's, still sitting unread, does not turn
    // tonight's into a silent edit of it.
    await cancel();
    await _plugin.show(
      id: nudgeNotificationId,
      title: nudgeTitle(count: count, atLeast: atLeast),
      body: nudgeBody,
      notificationDetails: _details(quiet: false),
      payload: nudgePayload,
    );
    return true;
  }

  @override
  Future<void> update({required int count, required bool atLeast}) => _plugin.show(
        id: nudgeNotificationId,
        title: nudgeTitle(count: count, atLeast: atLeast),
        body: nudgeBody,
        notificationDetails: _details(quiet: true),
        payload: nudgePayload,
      );

  @override
  Future<void> cancel() => _plugin.cancel(id: nudgeNotificationId);

  @override
  Future<bool> showing() async {
    final active = await _android?.getActiveNotifications() ?? const [];
    return active.any((n) => n.id == nudgeNotificationId);
  }
}
