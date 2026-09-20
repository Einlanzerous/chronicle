package dev.dodson.chronicle.capture

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import java.io.File

/**
 * Drives [CaptureService] from `adb` so CHRN-60's step-1 spike can run before
 * any UI exists. Debug builds only -- see `src/debug/AndroidManifest.xml`.
 *
 * ```
 * adb shell am start -n dev.dodson.chronicle/.MainActivity
 * adb shell am broadcast -a dev.dodson.chronicle.SPIKE \
 *     --es op start --es id cap1 --es container ogg --es policy OBSERVE
 * adb shell am broadcast -a dev.dodson.chronicle.SPIKE --es op stop
 * ```
 *
 * The `am start` is not decoration. Android 12+ refuses a foreground-service
 * start from the background, and a broadcast receiver is background -- so the
 * activity has to be visible for the `start` op to be allowed at all. Which is
 * itself a small confirmation of the plan's claim that capture belongs to a
 * visible activity and not to a tile or a widget.
 */
class SpikeReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        val op = intent.getStringExtra("op") ?: return
        when (op) {
            "start" -> start(context, intent)
            "stop" -> Log.i(TAG, "RESULT stop path=${CaptureService.stopRecording()}")
            "pause" -> Log.i(TAG, "RESULT pause ok=${CaptureService.pauseRecording()}")
            "resume" -> Log.i(TAG, "RESULT resume ok=${CaptureService.resumeRecording()}")
            "state" -> report(context)
            "ls" -> list(context)
            else -> Log.w(TAG, "unknown op $op")
        }
    }

    private fun start(context: Context, intent: Intent) {
        val id = intent.getStringExtra("id") ?: "spike"
        val container = intent.getStringExtra("container") ?: "ogg"
        val policy = intent.getStringExtra("policy") ?: "OBSERVE"
        val dir = File(File(context.filesDir, "captures"), id)
        dir.mkdirs()
        val audio = File(dir, if (container == "mp4") "audio.m4a" else "audio.opus")
        val lease = File(dir, "lease")

        Log.i(TAG, "RESULT start id=$id container=$container policy=$policy at=${System.currentTimeMillis()}")
        CaptureService.start(
            context = context,
            captureId = id,
            audioPath = audio.absolutePath,
            leasePath = lease.absolutePath,
            container = container,
            policy = runCatching { CaptureService.InterruptionPolicy.valueOf(policy) }
                .getOrDefault(CaptureService.InterruptionPolicy.OBSERVE),
            maxBytes = 0L,
        )
    }

    private fun report(context: Context) {
        val s = CaptureService.snapshot()
        Log.i(
            TAG,
            "RESULT state id=${s.captureId} state=${s.state} elapsedMs=${s.elapsedMs} " +
                "bytes=${s.byteSize} silenced=${s.silenced} micOpen=${s.micOpen} " +
                "amp=${s.amplitude} held=${CaptureRegistry.heldIds()} " +
                "instance=${CaptureRegistry.instanceId}",
        )
    }

    private fun list(context: Context) {
        val root = File(context.filesDir, "captures")
        val rows = root.listFiles()?.sortedBy { it.name }?.joinToString(" | ") { dir ->
            val files = dir.listFiles()?.joinToString(",") { "${it.name}:${it.length()}" } ?: ""
            "${dir.name}[$files]"
        } ?: "(none)"
        Log.i(TAG, "RESULT ls root=${root.absolutePath} $rows")
    }

    private companion object {
        const val TAG = "ChronicleSpike"
    }
}
