package dev.codexremote.app

internal class PendingCommands {
    private data class Pending(val command: RemoteCommand, val deadlineMs: Long)
    private val requests = mutableMapOf<String, Pending>()

    @Synchronized
    fun add(requestId: String, command: RemoteCommand, nowMs: Long) {
        requests[requestId] = Pending(command, nowMs + command.responseTimeoutMs)
    }

    @Synchronized
    fun remove(requestId: String): RemoteCommand? = requests.remove(requestId)?.command

    @Synchronized
    fun expire(nowMs: Long): List<Pair<String, RemoteCommand>> {
        val expired = requests.filterValues { it.deadlineMs <= nowMs }
        expired.keys.forEach { requests.remove(it) }
        return expired.map { (id, pending) -> id to pending.command }
    }

    @Synchronized
    fun clear() = requests.clear()
}
