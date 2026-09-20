package dev.dodson.chronicle.capture

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioTrack
import android.util.Log
import java.io.File
import kotlin.math.PI
import kotlin.math.sin

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
            "tone" -> tone(intent)
            else -> Log.w(TAG, "unknown op $op")
        }
    }

    private fun start(context: Context, intent: Intent) {
        val id = intent.getStringExtra("id") ?: "spike"
        val container = intent.getStringExtra("container") ?: "ogg"
        val policy = intent.getStringExtra("policy") ?: "OBSERVE"
        val dir = File(File(context.filesDir, "captures"), id)
        dir.mkdirs()

        Log.i(TAG, "RESULT start id=$id container=$container policy=$policy at=${System.currentTimeMillis()}")
        // Android refuses a foreground-service start unless the app is visible,
        // and an uncaught refusal kills the process -- which silently voids the
        // run AND the next few broadcasts, since they land in a fresh process
        // with an empty registry. Reported, not thrown.
        try {
            startService(context, id, container, policy, dir)
        } catch (e: Exception) {
            Log.e(TAG, "RESULT start REFUSED: ${e.javaClass.simpleName}: ${e.message}")
        }
    }

    private fun startService(
        context: Context,
        id: String,
        container: String,
        policy: String,
        dir: File,
    ) {
        val audio = File(dir, if (container == "mp4") "audio.m4a" else "audio.opus")
        val lease = File(dir, "lease")
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

    /**
     * Plays a synthetic tone through the phone's own speaker.
     *
     * The committed test fixture has to be recorded by a real device -- that is
     * the whole point of pinning the Dart trim and the Go probe to the same
     * bytes -- and Chronicle's repository is **public**, so it cannot be room
     * audio. A phone playing a generated tone into its own microphone is both:
     * genuinely device-written, and unambiguously not anybody's voice.
     *
     * Three tones rather than one steady sine, because a pure constant tone
     * compresses to almost nothing and would produce a fixture of uniformly
     * tiny pages -- which is not the page geometry the trim has to cope with.
     */
    private fun tone(intent: Intent) {
        val seconds = intent.getStringExtra("seconds")?.toIntOrNull() ?: 9
        val rate = 44_100
        val samples = ShortArray(rate * seconds)
        val steps = intArrayOf(440, 660, 880)
        for (i in samples.indices) {
            val hz = steps[(i / (samples.size / steps.size)).coerceIn(0, steps.size - 1)]
            samples[i] = (sin(2.0 * PI * hz * i / rate) * 0.6 * Short.MAX_VALUE).toInt().toShort()
        }
        val track = AudioTrack.Builder()
            .setAudioAttributes(
                AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_MEDIA)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
                    .build(),
            )
            .setAudioFormat(
                AudioFormat.Builder()
                    .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                    .setSampleRate(rate)
                    .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                    .build(),
            )
            .setBufferSizeInBytes(samples.size * 2)
            .setTransferMode(AudioTrack.MODE_STATIC)
            .build()
        track.write(samples, 0, samples.size)
        track.play()
        Log.i(TAG, "RESULT tone seconds=$seconds hz=${steps.joinToString(",")}")
    }

    private fun report(context: Context) {
        val s = CaptureService.snapshot()
        Log.i(
            TAG,
            "RESULT state id=${s.captureId} state=${s.state} elapsedMs=${s.elapsedMs} " +
                "bytes=${s.byteSize} silenced=${s.silenced} micOpen=${s.micOpen} " +
                "amp=${s.amplitude} hasConfig=${s.hasConfig} held=${CaptureRegistry.heldIds()} " +
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
