package dev.codexremote.app

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class UserFacingErrorTest {
    @Test
    fun turnsResumeConflictResponseIntoSessionNotice() {
        val payload = JSONObject()
            .put("ok", false)
            .put(
                "error",
                "app-server thread/resume: {\"code\":-32600,\"message\":\"thread 01a0d125-b762-70b0-ae2e-80170483fd3f already has an active writer\"}",
            )

        val error = UserFacingError.fromMessage(payload.getString("error"))

        assertTrue(error.sessionInUse)
        assertEquals("请先在电脑端或其他客户端退出该会话，再点击“重试”。也可以返回主界面，选择其他会话。", error.message)
        assertFalse(error.message.contains("active writer"))
    }

    @Test
    fun recognizesUnwrappedConflictNotification() {
        val error = UserFacingError.fromMessage("thread thread-1 already has an active writer")

        assertTrue(error.sessionInUse)
    }

    @Test
    fun closedComputerPipeOffersReconnectWithoutShowingRawError() {
        val messages = listOf(
            "write |1: file already closed",
            "app-server thread/resume: write |1 file already closed",
            "host is offline",
            "Codex app-server is not running",
            "Computer response timed out",
        )

        messages.forEach { message ->
            val error = UserFacingError.fromMessage(message)

            assertTrue(message, error.reconnectRequired)
            assertFalse(error.sessionInUse)
            assertTrue(error.message.contains("启动或重启电脑端程序"))
            assertFalse(error.message.contains("file already closed"))
        }
    }

    @Test
    fun relayDisconnectionPointsToNetworkRecovery() {
        val error = UserFacingError.fromMessage("relay is not connected")

        assertTrue(error.reconnectRequired)
        assertTrue(error.message.contains("检查网络"))
    }

    @Test
    fun preservesOtherErrorsWithTheSameRpcCode() {
        val message = "app-server thread/start: {\"code\":-32600,\"message\":\"runtimeWorkspaceRoots requires experimentalApi capability\"}"

        val error = UserFacingError.fromMessage(message)

        assertFalse(error.sessionInUse)
        assertEquals(message, error.message)
    }
}
