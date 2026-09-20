package dev.dodson.chronicle.capture

import android.Manifest
import android.app.Activity
import android.content.Context
import android.content.pm.PackageManager
import android.os.StatFs
import io.flutter.plugin.common.BinaryMessenger
import io.flutter.plugin.common.EventChannel
import io.flutter.plugin.common.MethodCall
import io.flutter.plugin.common.MethodChannel
import java.io.File

/**
 * The seam between Dart and [CaptureService].
 *
 * Deliberately thin: every decision that can be made in Dart is made in Dart,
 * because Dart is the half `flutter test` can reach. What has to be here is
 * what only the platform knows — whether the microphone is really open, how
 * much space is left, and **whether a live recorder still holds a capture**.
 *
 * That last one is why [isHeld] exists and why it needs no deadline. The
 * service runs in this same process, so [CaptureRegistry] is a direct read: a
 * `false` means nobody is recording it, full stop, and a force-stopped process
 * comes back with an empty registry. Recovery after a kill therefore waits for
 * nothing.
 */
class CaptureChannel(private val activity: Activity) {

    private val context: Context get() = activity.applicationContext

    companion object {
        private const val METHODS = "dev.dodson.chronicle/capture"
        private const val EVENTS = "dev.dodson.chronicle/capture/events"

        /**
         * Leave this much free whatever happens.
         *
         * A recorder that fills the last block of a phone takes the rest of the
         * system down with it, and the memo it was saving is not worth that.
         */
        private const val FREE_SPACE_RESERVE = 256L * 1024 * 1024

        /** CHRN-20's `DefaultMaxBytes`: one declaration may not exceed 1 GiB. */
        private const val SERVER_MAX_BYTES = 1024L * 1024 * 1024

        private const val PERMISSION_REQUEST = 0x0C60
    }

    private var pendingPermission: MethodChannel.Result? = null

    private var events: EventChannel.EventSink? = null

    fun attach(messenger: BinaryMessenger) {
        MethodChannel(messenger, METHODS).setMethodCallHandler { call, result ->
            handle(call, result)
        }
        EventChannel(messenger, EVENTS).setStreamHandler(
            object : EventChannel.StreamHandler {
                override fun onListen(arguments: Any?, sink: EventChannel.EventSink?) {
                    events = sink
                    CaptureService.listener = { snapshot -> events?.success(snapshot.toMap()) }
                    // Emitted immediately so a UI that was recreated re-attaches
                    // to a recording already in flight rather than showing idle
                    // until the next heartbeat.
                    sink?.success(CaptureService.snapshot().toMap())
                }

                override fun onCancel(arguments: Any?) {
                    CaptureService.listener = null
                    events = null
                }
            },
        )
    }

    private fun handle(call: MethodCall, result: MethodChannel.Result) {
        when (call.method) {
            "capturesRoot" -> result.success(capturesRoot().absolutePath)

            "freeBytes" -> result.success(freeBytes())

            "start" -> {
                val id = call.argument<String>("captureId")
                if (id == null) {
                    result.error("bad_args", "captureId is required", null)
                    return
                }
                val dir = File(capturesRoot(), id).apply { mkdirs() }
                val budget = (freeBytes() - FREE_SPACE_RESERVE)
                    .coerceAtMost(SERVER_MAX_BYTES)
                    .coerceAtLeast(0)
                CaptureService.start(
                    context = context,
                    captureId = id,
                    audioPath = File(dir, "audio.opus").absolutePath,
                    leasePath = File(dir, "lease").absolutePath,
                    container = "ogg",
                    policy = runCatching {
                        CaptureService.InterruptionPolicy.valueOf(
                            call.argument<String>("policy")
                                ?: CaptureService.InterruptionPolicy.PAUSE_RESUME.name,
                        )
                    }.getOrDefault(CaptureService.InterruptionPolicy.PAUSE_RESUME),
                    maxBytes = budget,
                )
                result.success(null)
            }

            "hasMicPermission" -> result.success(hasMicPermission())

            "requestPermissions" -> requestPermissions(result)

            "stop" -> result.success(CaptureService.stopRecording())
            "pause" -> result.success(CaptureService.pauseRecording())
            "resume" -> result.success(CaptureService.resumeRecording())
            "state" -> result.success(CaptureService.snapshot().toMap())

            "isHeld" -> {
                val id = call.argument<String>("captureId")
                if (id == null) {
                    result.error("bad_args", "captureId is required", null)
                } else {
                    result.success(CaptureRegistry.isHeld(id))
                }
            }

            "heldIds" -> result.success(CaptureRegistry.heldIds())

            else -> result.notImplemented()
        }
    }

    private fun hasMicPermission(): Boolean =
        context.checkSelfPermission(Manifest.permission.RECORD_AUDIO) ==
            PackageManager.PERMISSION_GRANTED

    /**
     * Asks for the microphone, and for notifications alongside it.
     *
     * Only the microphone answer is reported back. A refused notification
     * permission costs the visible recording indicator and nothing else -- a
     * foreground service runs either way -- and gating capture on it would
     * reintroduce, in a different costume, the dependency this epic exists to
     * remove. Asked at the first tap, never at launch.
     */
    private fun requestPermissions(result: MethodChannel.Result) {
        if (hasMicPermission()) {
            result.success(true)
            return
        }
        pendingPermission?.success(false) // a second ask supersedes the first
        pendingPermission = result
        activity.requestPermissions(
            arrayOf(
                Manifest.permission.RECORD_AUDIO,
                Manifest.permission.POST_NOTIFICATIONS,
            ),
            PERMISSION_REQUEST,
        )
    }

    /** Forwarded from `MainActivity`, which is the only thing Android tells. */
    fun onRequestPermissionsResult(requestCode: Int): Boolean {
        if (requestCode != PERMISSION_REQUEST) return false
        pendingPermission?.success(hasMicPermission())
        pendingPermission = null
        return true
    }

    /**
     * App-internal storage, and the path Dart is told rather than guesses.
     *
     * One source of truth for where captures live: a Dart-side `path_provider`
     * lookup and a Kotlin-side `filesDir` that disagreed by one directory would
     * mean the recorder writing where recovery never looks, which is the worst
     * possible way to lose a memo — the bytes would be on the disk the whole
     * time.
     */
    private fun capturesRoot(): File = File(context.filesDir, "captures")

    private fun freeBytes(): Long {
        val stat = StatFs(context.filesDir.absolutePath)
        return stat.availableBlocksLong * stat.blockSizeLong
    }

    private fun CaptureService.Snapshot.toMap(): Map<String, Any?> = mapOf(
        "captureId" to captureId,
        "state" to state.name.lowercase(),
        "elapsedMs" to elapsedMs,
        "byteSize" to byteSize,
        "silenced" to silenced,
        "micOpen" to micOpen,
        "hasConfig" to hasConfig,
        "amplitude" to amplitude,
    )
}
