package dev.dodson.chronicle.power

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.os.PowerManager
import android.provider.Settings
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel

/**
 * CHRN-146: whether Android's battery optimisation lets this app's background
 * wake run, and the system's own request to lift it.
 *
 * Dart cannot know either. With Adaptive Battery's saver forcing apps into
 * standby, the WorkManager job behind the process-dead upload retry and the
 * evening nudge is held (`BACKGROUND_NOT_RESTRICTED` unsatisfied) unless the
 * package is on the device-idle whitelist, which is what this exemption is.
 */
class BatteryChannel(private val activity: Activity) {

    companion object {
        private const val METHODS = "dev.dodson.chronicle/battery"
    }

    fun attach(messenger: BinaryMessenger) {
        MethodChannel(messenger, METHODS).setMethodCallHandler(::onCall)
    }

    private fun onCall(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "isIgnoringBatteryOptimizations" -> result.success(isIgnoring())
            "requestIgnoreBatteryOptimizations" -> result.success(request())
            else -> result.notImplemented()
        }
    }

    private fun isIgnoring(): Boolean {
        val pm = activity.getSystemService(PowerManager::class.java) ?: return false
        return pm.isIgnoringBatteryOptimizations(activity.packageName)
    }

    /**
     * Opens the system's one-tap dialog. Returns whether a screen was opened;
     * the answer itself is never delivered here -- Dart re-reads [isIgnoring]
     * when the app resumes, so a refusal and an accept are told apart by state
     * and not by a result code.
     */
    private fun request(): Boolean {
        val direct = Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)
            .setData(Uri.parse("package:${activity.packageName}"))
        return try {
            activity.startActivity(direct)
            true
        } catch (_: ActivityNotFoundException) {
            // Some OEM builds ship no handler for the direct request; the
            // general list is the same switch, one step further away.
            try {
                activity.startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS))
                true
            } catch (_: ActivityNotFoundException) {
                false
            }
        }
    }
}
