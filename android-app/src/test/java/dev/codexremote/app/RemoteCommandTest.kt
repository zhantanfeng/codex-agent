package dev.codexremote.app

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RemoteCommandTest {
    @Test
    fun capturesThreadRequestForRetry() {
        val original = JSONObject().put("threadId", "thread-1")
        val command = RemoteCommand.capture("thread.resume", original)
        original.put("threadId", "changed")

        assertEquals("thread-1", command.data().getString("threadId"))
        assertTrue(command.retryable)
    }

    @Test
    fun doesNotRetryTurnStart() {
        val command = RemoteCommand.capture("turn.start", JSONObject().put("text", "hello"))

        assertFalse(command.retryable)
    }
}
