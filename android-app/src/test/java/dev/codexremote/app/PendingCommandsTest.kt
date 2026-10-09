package dev.codexremote.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class PendingCommandsTest {
    @Test
    fun unansweredRefreshExpiresOnceAfterFifteenSeconds() {
        val pending = PendingCommands()
        val command = RemoteCommand("threads.list", "{}")
        pending.add("refresh", command, nowMs = 1_000)

        assertTrue(pending.expire(15_999).isEmpty())
        assertEquals(listOf("refresh" to command), pending.expire(16_000))
        assertTrue(pending.expire(20_000).isEmpty())
    }

    @Test
    fun completedCommandNeverProducesTimeoutNotice() {
        val pending = PendingCommands()
        val command = RemoteCommand("projects.list", "{}")
        pending.add("refresh", command, nowMs = 0)

        assertEquals(command, pending.remove("refresh"))
        assertTrue(pending.expire(100_000).isEmpty())
    }

    @Test
    fun turnCommandWaitsForTheAgentTimeoutAndCannotBeRetried() {
        val pending = PendingCommands()
        val command = RemoteCommand("turn.start", "{}")
        pending.add("turn", command, nowMs = 0)

        assertTrue(pending.expire(60_000).isEmpty())
        assertEquals(listOf("turn" to command), pending.expire(65_000))
        assertTrue(!command.retryable)
    }

    @Test
    fun reconnectDiscardsOldRequestsAndLateResponses() {
        val pending = PendingCommands()
        val oldCommand = RemoteCommand("thread.resume", "{}")
        val newCommand = RemoteCommand("threads.list", "{}")
        pending.add("old", oldCommand, nowMs = 0)

        pending.clear()
        pending.add("new", newCommand, nowMs = 1_000)

        assertNull(pending.remove("old"))
        assertEquals(newCommand, pending.remove("new"))
        assertTrue(pending.expire(100_000).isEmpty())
    }
}
