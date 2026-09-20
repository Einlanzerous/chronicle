package dev.dodson.chronicle.capture

import java.util.UUID

/**
 * Which captures a live recorder currently holds, and who is holding them.
 *
 * **This is the thing that makes recovery safe, and it is deliberately the
 * dumbest possible object.** CHRN-60's first draft classified an interrupted
 * capture by reading `state: recording` off the disk, which cannot tell a crash
 * from a swipe-away the moment a foreground service is allowed to outlive the
 * UI -- and the service exists precisely so that it does. Recovery would then
 * trim a file that [CaptureService] was still appending to.
 *
 * The fix is not a cleverer timer. It is that recovery and the service run in
 * the **same process**: `AndroidManifest.xml` declares no `android:process` for
 * the service, so a static set here answers "is anybody recording this?"
 * immediately, with no IPC and no deadline. A force-stopped process takes the
 * set with it and a fresh process starts empty, which is exactly the
 * classification recovery wants -- and it is why "relaunch and recover with no
 * wait" is true by construction rather than by tuning.
 *
 * The on-disk `lease` file is the fallback for the case this object cannot
 * cover, and it is written for the same reason a belt is worn with braces:
 * nothing here survives the process, so anything asking across a process
 * boundary has only the lease to go on.
 */
object CaptureRegistry {

    /**
     * Minted once per service start, never per capture.
     *
     * A device boot id was the first idea and it distinguishes nothing useful:
     * it tells a reboot from a live device, and says nothing at all about a
     * killed service versus a running one inside the same boot -- which is the
     * only question a lease has to answer. An instance id makes a previous
     * instance's lease recognisably dead rather than merely old.
     */
    val instanceId: String = UUID.randomUUID().toString()

    private val held = mutableSetOf<String>()

    @Synchronized
    fun hold(captureId: String) {
        held.add(captureId)
    }

    @Synchronized
    fun release(captureId: String) {
        held.remove(captureId)
    }

    /**
     * True when a live recorder in this process holds [captureId].
     *
     * False is a **definite** answer, not a timeout: if nothing in this process
     * holds it, either the recorder released it or the process that held it is
     * gone. Both mean the same thing to recovery.
     */
    @Synchronized
    fun isHeld(captureId: String): Boolean = held.contains(captureId)

    @Synchronized
    fun heldIds(): List<String> = held.toList()
}
