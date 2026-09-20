package dev.dodson.chronicle.capture

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.AudioManager
import android.media.AudioRecordingConfiguration
import android.media.MediaRecorder
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.os.SystemClock
import android.util.Log
import dev.dodson.chronicle.MainActivity
import java.io.File
import java.io.FileOutputStream
import java.io.IOException

/**
 * The recorder, and everything that has to agree with it.
 *
 * ## Why a service in this repo rather than a plugin
 *
 * Chronicle's Android client is Flutter, and the estate already runs
 * `flutter_foreground_task` in Argosy, so a plugin pair was the obvious call and
 * CHRN-60 weighed it seriously. The reason it went the other way is that this
 * ticket's correctness is a claim about **which container the bytes land in and
 * when they are flushed**, and about **who can be asked whether a capture is
 * still being written** -- and one object has to hold all of it. Six things that
 * a plugin's opinion would otherwise sit in front of live here together: the
 * silenced-microphone signal, the amplitude the waveform is drawn from, the
 * elapsed clock, the live file size, the fsync cadence, and
 * [CaptureRegistry].
 *
 * ## The container is the decision
 *
 * Ogg, not MPEG-4, and the argument is structural rather than about quality.
 * MPEG-4 writes its index at `stop()`, so a recording killed before stop is a
 * file with no index: not playable, not probeable, not repairable on a phone.
 * Ogg is a chain of self-describing pages, so a truncated file is still a valid
 * stream up to its last complete page -- which is what the Dart-side trim
 * recovers, and what the server's own probe can then describe with no decoder
 * at all (`internal/audio/probe.go`).
 *
 * ## Two loss windows, two mechanisms
 *
 * Bytes the encoder has not yet handed to the kernel die with the process;
 * `fsync` cannot flush what was never written, so only owning the page writer
 * would bound that, and CHRN-60 measures it rather than assuming it. Bytes the
 * kernel holds but has not written back die with the **device** -- a flat
 * battery, a panic -- and `fsync` is exactly what bounds those. This service
 * therefore owns the output stream rather than handing `MediaRecorder` a path,
 * so there is a file descriptor to sync on a cadence. That is the only reason
 * for the [FileOutputStream] below; `setOutputFile(File)` would have been
 * shorter and would have left row 6 unbounded.
 */
class CaptureService : Service() {

    /** What to do when something else takes the microphone. */
    enum class InterruptionPolicy {
        /** Pause on silenced, resume when it clears. CHRN-60's approved pick. */
        PAUSE_RESUME,

        /** Pause on silenced; only a person resumes. The pre-committed fallback. */
        PAUSE_MANUAL,

        /** End the memo at the interruption. The second pre-committed fallback. */
        STOP,

        /**
         * Record the signal, act on nothing.
         *
         * This exists for the step-1 spike, which has to observe what a paused
         * recorder reports **without** a pause policy confounding the reading.
         */
        OBSERVE,
    }

    enum class State { IDLE, RECORDING, PAUSED }

    /** A flat snapshot, because everything reading this is on another thread. */
    data class Snapshot(
        val captureId: String?,
        val state: State,
        val elapsedMs: Long,
        val byteSize: Long,
        val silenced: Boolean,
        val micOpen: Boolean,
        val amplitude: Int,
    )

    companion object {
        private const val TAG = "ChronicleCapture"

        const val EXTRA_CAPTURE_ID = "capture_id"
        const val EXTRA_AUDIO_PATH = "audio_path"
        const val EXTRA_LEASE_PATH = "lease_path"
        const val EXTRA_CONTAINER = "container"
        const val EXTRA_POLICY = "policy"
        const val EXTRA_MAX_BYTES = "max_bytes"

        private const val ACTION_START = "dev.dodson.chronicle.capture.START"
        const val ACTION_STOP = "dev.dodson.chronicle.capture.STOP"

        private const val CHANNEL_ID = "chronicle_capture"
        private const val NOTIFICATION_ID = 0x0C60

        /** How often the lease is refreshed and the audio fsync'd. */
        const val HEARTBEAT_MS = 5_000L

        /**
         * Mono voice Opus.
         *
         * Measured on device rather than taken from the bitrate: a 13.2 s
         * recording came to 53,769 bytes, which is ~4.1 kB/s -- about 15 MB an
         * hour and a ~5 MB twenty-minute memo, once Ogg page overhead and VBR
         * are in. CHRN-20's 1 GiB cap is therefore ~68 hours and is not a real
         * bound on anything.
         */
        private const val SAMPLE_RATE = 48_000
        private const val BIT_RATE = 24_000

        @Volatile
        private var current: CaptureService? = null

        /** Set by the platform channel so state can reach Dart as it changes. */
        @Volatile
        var listener: ((Snapshot) -> Unit)? = null

        fun start(
            context: Context,
            captureId: String,
            audioPath: String,
            leasePath: String,
            container: String,
            policy: InterruptionPolicy,
            maxBytes: Long,
        ) {
            val intent = Intent(context, CaptureService::class.java).apply {
                action = ACTION_START
                putExtra(EXTRA_CAPTURE_ID, captureId)
                putExtra(EXTRA_AUDIO_PATH, audioPath)
                putExtra(EXTRA_LEASE_PATH, leasePath)
                putExtra(EXTRA_CONTAINER, container)
                putExtra(EXTRA_POLICY, policy.name)
                putExtra(EXTRA_MAX_BYTES, maxBytes)
            }
            // Started from a visible activity, which is what Android 12+'s
            // foreground-service start restrictions require. A quick-settings
            // tile or a widget would not be, which is why neither is in scope.
            context.startForegroundService(intent)
        }

        fun snapshot(): Snapshot = current?.snapshotNow() ?: Snapshot(
            captureId = null,
            state = State.IDLE,
            elapsedMs = 0,
            byteSize = 0,
            silenced = false,
            micOpen = false,
            amplitude = 0,
        )

        /** Returns the finished audio path, or null when nothing was recording. */
        fun stopRecording(): String? = current?.finish()

        fun pauseRecording(): Boolean = current?.pauseNow(byPerson = true) ?: false

        fun resumeRecording(): Boolean = current?.resumeNow() ?: false
    }

    private var recorder: MediaRecorder? = null
    private var output: FileOutputStream? = null
    private var audioFile: File? = null
    private var leaseFile: File? = null

    private var captureId: String? = null
    private var policy: InterruptionPolicy = InterruptionPolicy.PAUSE_RESUME
    private var state: State = State.IDLE

    /**
     * The clock is [SystemClock.elapsedRealtime], never wall clock.
     *
     * A memo that appears to run backwards because NTP corrected the device
     * mid-recording is a small bug with a very bad look, and `MediaRecorder`
     * offers no position getter of its own to borrow instead.
     */
    private var startedAtElapsed: Long = 0
    private var pausedTotalMs: Long = 0
    private var pausedAtElapsed: Long = 0

    private var silenced: Boolean = false
    private var micOpen: Boolean = false
    private var lastAmplitude: Int = 0

    /** True when the pause was the platform's doing, so a resume may undo it. */
    private var pausedByInterruption: Boolean = false

    private var wakeLock: PowerManager.WakeLock? = null
    private val handler = Handler(Looper.getMainLooper())

    private val recordingCallback = object : AudioManager.AudioRecordingCallback() {
        override fun onRecordingConfigChanged(configs: MutableList<AudioRecordingConfiguration>?) {
            // Registered on the RECORDER, not on AudioManager: the global
            // callback reports every app's configurations, and "is MY microphone
            // open" is not answerable from that. MediaRecorder has implemented
            // AudioRecordingMonitor since API 29, which is inside this app's
            // minSdk of 30.
            val config = try {
                recorder?.activeRecordingConfiguration
            } catch (e: Exception) {
                Log.w(TAG, "reading the active recording configuration failed", e)
                null
            }
            val nowSilenced = config?.isClientSilenced ?: false
            val nowOpen = config != null && !nowSilenced

            // Logged unconditionally, including under OBSERVE, because whether a
            // PAUSED recorder still reports a configuration at all is the
            // question the spike exists to answer -- it is the only resume
            // signal available at this minSdk. READ_PHONE_STATE is a permission
            // a memo app should not be asking for, and OnModeChangedListener is
            // API 31.
            Log.i(
                TAG,
                "recording-config state=$state config=${config != null} " +
                    "silenced=$nowSilenced open=$nowOpen",
            )

            val wasSilenced = silenced
            silenced = nowSilenced
            micOpen = nowOpen

            when {
                nowSilenced && !wasSilenced -> onMicrophoneTaken()
                !nowSilenced && wasSilenced -> onMicrophoneReturned()
            }
            emit()
        }
    }

    private val heartbeat = object : Runnable {
        override fun run() {
            if (state == State.IDLE) return

            // Row 6 of the loss table: the page cache is lost when the DEVICE
            // dies, not when the process does. At ~3 KB/s the flash cost of
            // syncing every few seconds is nil, and it puts the power-loss
            // window in the same range as the encoder window the spike measures.
            try {
                output?.fd?.sync()
            } catch (e: IOException) {
                Log.w(TAG, "fsync of the capture failed", e)
            }

            writeLease()
            sampleAmplitude()
            emit()
            handler.postDelayed(this, HEARTBEAT_MS)
        }
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        current = this
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_START -> handleStart(intent)
            ACTION_STOP -> finish()
        }
        // START_NOT_STICKY: if Android kills this process, it must NOT resurrect
        // the service with a null intent and no recorder. Recovery on next
        // launch is the path that handles a killed recording, and it is written
        // to expect the process to have gone -- a restarted empty service would
        // put a live registry entry behind a capture nothing is writing.
        return START_NOT_STICKY
    }

    private fun handleStart(intent: Intent) {
        if (state != State.IDLE) {
            Log.w(TAG, "start ignored: already ${state.name.lowercase()}")
            return
        }

        val id = intent.getStringExtra(EXTRA_CAPTURE_ID) ?: return
        val audioPath = intent.getStringExtra(EXTRA_AUDIO_PATH) ?: return
        val leasePath = intent.getStringExtra(EXTRA_LEASE_PATH) ?: return
        val container = intent.getStringExtra(EXTRA_CONTAINER) ?: "ogg"
        val maxBytes = intent.getLongExtra(EXTRA_MAX_BYTES, 0L)
        policy = runCatching {
            InterruptionPolicy.valueOf(
                intent.getStringExtra(EXTRA_POLICY) ?: InterruptionPolicy.PAUSE_RESUME.name,
            )
        }.getOrDefault(InterruptionPolicy.PAUSE_RESUME)

        captureId = id
        audioFile = File(audioPath)
        leaseFile = File(leasePath)

        startForeground(
            NOTIFICATION_ID,
            buildNotification(),
            ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE,
        )

        try {
            beginRecording(container, maxBytes)
        } catch (e: Exception) {
            Log.e(TAG, "starting the recorder failed", e)
            teardown()
            stopSelf()
            return
        }

        // Held only once the recorder is actually running. A capture in the
        // registry that nothing is writing would be worse than one that is
        // missing: recovery would decline to salvage it, forever.
        CaptureRegistry.hold(id)

        acquireWakeLock()
        writeLease()
        handler.postDelayed(heartbeat, HEARTBEAT_MS)
        emit()
    }

    private fun beginRecording(container: String, maxBytes: Long) {
        val file = audioFile ?: return
        file.parentFile?.mkdirs()

        val fos = FileOutputStream(file)
        output = fos

        val rec = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            MediaRecorder(this)
        } else {
            @Suppress("DEPRECATION")
            MediaRecorder()
        }
        recorder = rec

        rec.setAudioSource(MediaRecorder.AudioSource.MIC)
        if (container == "mp4") {
            // The spike's control arm, and nothing else. It exists to tell "the
            // writer tore" apart from "the platform finalised the file when the
            // client died" -- if an app kill leaves a PLAYABLE mp4, then the
            // media server finalises on client death and the trim path would
            // never see a torn file on this device, which changes what the
            // device evidence is worth. Never reachable from the app itself.
            rec.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            rec.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
        } else {
            rec.setOutputFormat(MediaRecorder.OutputFormat.OGG)
            rec.setAudioEncoder(MediaRecorder.AudioEncoder.OPUS)
        }
        rec.setAudioChannels(1)
        rec.setAudioSamplingRate(SAMPLE_RATE)
        rec.setAudioEncodingBitRate(BIT_RATE)
        rec.setOutputFile(fos.fd)

        if (maxBytes > 0) {
            // A clean stop on the limit, rather than an ENOSPC write error that
            // ends the recording in whatever state the writer happened to be in.
            runCatching { rec.setMaxFileSize(maxBytes) }
                .onFailure { Log.w(TAG, "setMaxFileSize refused", it) }
        }
        rec.setOnInfoListener { _, what, _ ->
            if (what == MediaRecorder.MEDIA_RECORDER_INFO_MAX_FILESIZE_REACHED ||
                what == MediaRecorder.MEDIA_RECORDER_INFO_MAX_FILESIZE_APPROACHING
            ) {
                Log.w(TAG, "file size limit reached; finishing cleanly")
                handler.post { finish() }
            }
        }
        rec.setOnErrorListener { _, what, extra ->
            Log.e(TAG, "recorder error what=$what extra=$extra; finishing cleanly")
            handler.post { finish() }
        }

        rec.prepare()

        val startCalledAt = SystemClock.elapsedRealtime()
        rec.start()
        Log.i(TAG, "start() returned after ${SystemClock.elapsedRealtime() - startCalledAt}ms")

        rec.registerAudioRecordingCallback(mainExecutor, recordingCallback)

        startedAtElapsed = startCalledAt
        pausedTotalMs = 0
        state = State.RECORDING
    }

    private fun onMicrophoneTaken() {
        Log.i(TAG, "microphone taken (policy=$policy)")
        when (policy) {
            InterruptionPolicy.PAUSE_RESUME,
            InterruptionPolicy.PAUSE_MANUAL,
            -> pauseNow(byPerson = false)

            InterruptionPolicy.STOP -> finish()
            InterruptionPolicy.OBSERVE -> Unit
        }
    }

    private fun onMicrophoneReturned() {
        Log.i(TAG, "microphone returned (policy=$policy)")
        if (policy == InterruptionPolicy.PAUSE_RESUME && pausedByInterruption) {
            resumeNow()
        }
    }

    /**
     * Pause, keeping ONE logical Ogg stream.
     *
     * The obvious implementation of pause -- stop the recorder, start a second
     * one, concatenate the files -- produces exactly the shape the server
     * refuses. `internal/audio/probe.go` declines a chained stream by
     * arithmetic, because granules restart per link and no single number
     * describes the file, so a concatenated memo would arrive with no duration,
     * no codec and no sample rate. `MediaRecorder.pause()` is the only
     * mechanism that keeps it one stream.
     */
    private fun pauseNow(byPerson: Boolean): Boolean {
        if (state != State.RECORDING) return false
        return try {
            recorder?.pause()
            pausedAtElapsed = SystemClock.elapsedRealtime()
            pausedByInterruption = !byPerson
            state = State.PAUSED
            writeLease()
            emit()
            true
        } catch (e: IllegalStateException) {
            Log.w(TAG, "pause refused", e)
            false
        }
    }

    private fun resumeNow(): Boolean {
        if (state != State.PAUSED) return false
        return try {
            recorder?.resume()
            pausedTotalMs += SystemClock.elapsedRealtime() - pausedAtElapsed
            pausedByInterruption = false
            state = State.RECORDING
            writeLease()
            emit()
            true
        } catch (e: IllegalStateException) {
            Log.w(TAG, "resume refused", e)
            false
        }
    }

    private fun finish(): String? {
        if (state == State.IDLE) return null
        val path = audioFile?.absolutePath

        val stopCalledAt = SystemClock.elapsedRealtime()
        try {
            recorder?.unregisterAudioRecordingCallback(recordingCallback)
            recorder?.stop()
        } catch (e: RuntimeException) {
            // stop() throws when too little was captured for the writer to
            // produce anything. The bytes on disk are still whatever landed, and
            // the Dart side decides what they are worth -- a capture that
            // recovered nothing is marked `empty` and KEPT, never cleaned up,
            // because that remnant is the only visible trace of a memo whose
            // audio never left the encoder.
            Log.w(TAG, "stop() threw; keeping whatever reached the file", e)
        }
        Log.i(TAG, "stop() returned after ${SystemClock.elapsedRealtime() - stopCalledAt}ms")

        try {
            output?.fd?.sync()
        } catch (e: IOException) {
            Log.w(TAG, "final fsync failed", e)
        }

        teardown()
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
        emit()
        return path
    }

    private fun teardown() {
        handler.removeCallbacks(heartbeat)
        runCatching { recorder?.release() }
        recorder = null
        runCatching { output?.close() }
        output = null
        captureId?.let { CaptureRegistry.release(it) }
        captureId = null
        state = State.IDLE
        silenced = false
        micOpen = false
        releaseWakeLock()
    }

    override fun onDestroy() {
        if (state != State.IDLE) finish()
        current = null
        super.onDestroy()
    }

    private fun snapshotNow(): Snapshot = Snapshot(
        captureId = captureId,
        state = state,
        elapsedMs = elapsedMs(),
        byteSize = audioFile?.length() ?: 0L,
        silenced = silenced,
        micOpen = micOpen,
        amplitude = lastAmplitude,
    )

    private fun elapsedMs(): Long {
        if (state == State.IDLE) return 0
        val paused = if (state == State.PAUSED) {
            pausedTotalMs + (SystemClock.elapsedRealtime() - pausedAtElapsed)
        } else {
            pausedTotalMs
        }
        return SystemClock.elapsedRealtime() - startedAtElapsed - paused
    }

    private fun sampleAmplitude() {
        lastAmplitude = try {
            if (state == State.RECORDING) recorder?.maxAmplitude ?: 0 else 0
        } catch (e: IllegalStateException) {
            0
        }
    }

    private fun emit() {
        val snapshot = snapshotNow()
        handler.post { listener?.invoke(snapshot) }
    }

    /**
     * The lease: what a reader in ANOTHER process has to go on.
     *
     * In this process [CaptureRegistry] answers directly and this file is not
     * consulted. It is written anyway because the registry cannot outlive the
     * process, and a stale lease plus an instance id that no longer matches is
     * how anything else tells a dead recorder from a live one.
     */
    private fun writeLease() {
        val file = leaseFile ?: return
        val id = captureId ?: return
        val body = """{"instance_id":"${CaptureRegistry.instanceId}",""" +
            """"capture_id":"$id",""" +
            """"state":"${state.name.lowercase()}",""" +
            """"heartbeat_at":${System.currentTimeMillis()},""" +
            """"heartbeat_ms":$HEARTBEAT_MS}"""
        try {
            val tmp = File(file.parentFile, "${file.name}.tmp")
            FileOutputStream(tmp).use { out ->
                out.write(body.toByteArray())
                out.fd.sync()
            }
            if (!tmp.renameTo(file)) {
                Log.w(TAG, "lease rename failed")
            }
        } catch (e: IOException) {
            Log.w(TAG, "writing the lease failed", e)
        }
    }

    private fun acquireWakeLock() {
        val power = getSystemService(Context.POWER_SERVICE) as PowerManager
        wakeLock = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "chronicle:capture").apply {
            setReferenceCounted(false)
            acquire(4 * 60 * 60 * 1000L)
        }
    }

    private fun releaseWakeLock() {
        runCatching { wakeLock?.takeIf { it.isHeld }?.release() }
        wakeLock = null
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID,
            "Recording",
            NotificationManager.IMPORTANCE_LOW,
        ).apply {
            description = "Shown while Chronicle is recording a memo."
            setShowBadge(false)
        }
        (getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager)
            .createNotificationChannel(channel)
    }

    private fun buildNotification(): Notification {
        val open = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_NEW_TASK
            },
            PendingIntent.FLAG_IMMUTABLE,
        )
        // A STOP action, and it is not decoration. This service deliberately
        // outlives the UI -- swiping the app out of Recents leaves a recording
        // running rather than ending a thought mid-sentence -- which only works
        // if the notification is a real way to end it. Without this, a
        // recording whose UI is gone can be stopped only by force-stopping the
        // app, and a person who does not know that will believe they have lost
        // control of their own microphone.
        val stop = PendingIntent.getService(
            this,
            1,
            Intent(this, CaptureService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_IMMUTABLE,
        )
        return Notification.Builder(this, CHANNEL_ID)
            .setContentTitle("Recording")
            .setContentText("Chronicle is capturing a memo.")
            .setSmallIcon(android.R.drawable.presence_audio_online)
            .setContentIntent(open)
            .addAction(
                Notification.Action.Builder(null, "Stop", stop).build(),
            )
            .setOngoing(true)
            .build()
    }
}
