package dev.codexremote.app

// Only verified computer traffic counts as liveness. A turn can run for hours
// without producing text; the presence replies still keep the connection alive.
internal class ConnectionHealth {
    private var lastResponseMs = 0L
    private var backgroundedAtMs: Long? = null

    @Synchronized
    fun received(nowMs: Long) {
        lastResponseMs = nowMs
    }

    @Synchronized
    fun isOffline(nowMs: Long): Boolean = nowMs - lastResponseMs >= 15_000L

    @Synchronized
    fun needsReconnect(nowMs: Long): Boolean = nowMs - lastResponseMs >= 45_000L

    @Synchronized
    fun backgrounded(nowMs: Long) {
        backgroundedAtMs = nowMs
    }

    @Synchronized
    fun needsForegroundReconnect(nowMs: Long, hasSocket: Boolean, connected: Boolean): Boolean {
        val wasSuspended = backgroundedAtMs?.let { nowMs - it >= 15_000L } == true
        backgroundedAtMs = null
        // A socket can look open after Android suspended its network. Rebuild
        // it on return instead of waiting for the next ping failure.
        return !hasSocket || wasSuspended || (connected && isOffline(nowMs))
    }
}
