package dev.codexremote.app

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectionStateTest {
    private val thread = ThreadUi("thread-1", "title", "D:/project", "active", 0)
    private val conversation = UiState(
        connection = RemoteClient.ConnectionStatus.CONNECTED,
        selectedThread = thread,
        messages = listOf(MessageUi("reply", "assistant", "partial reply")),
        activeTurnId = "turn-1",
        threadLoaded = true,
        draftText = "unsent draft",
    )

    @Test
    fun backgroundDisconnectAndReconnectPreserveConversationAndClearOnlyTheStaleNotice() {
        val disconnected = applyConnectionStatus(conversation, RemoteClient.ConnectionStatus.ERROR, "network suspended")
        assertFalse(disconnected.canWriteThread)
        assertTrue(disconnected.error?.transportUnavailable == true)
        assertEquals(conversation.messages, disconnected.messages)
        assertEquals(conversation.draftText, disconnected.draftText)
        assertEquals(conversation.activeTurnId, disconnected.activeTurnId)

        val connecting = applyConnectionStatus(disconnected, RemoteClient.ConnectionStatus.CONNECTING, "")
        assertNull(connecting.error)
        val connected = applyConnectionStatus(connecting, RemoteClient.ConnectionStatus.CONNECTED, "")
        assertNull(connected.error)
        assertFalse(connected.threadLoaded) // History must be restored before sending is enabled.
        assertEquals(conversation.selectedThread, connected.selectedThread)
        assertEquals(conversation.messages, connected.messages)
        assertEquals(conversation.draftText, connected.draftText)
    }

    @Test
    fun verifiedHostTrafficClearsLeftoverTransportNoticeWithoutHidingOtherFailures() {
        val stale = conversation.copy(error = UserFacingError.connectionUnavailable())
        assertNull(applyRemotePresence(stale, online = true).error)
        val validationError = UserFacingError("invalid directory")
        val invalid = conversation.copy(error = validationError)
        assertEquals(validationError, applyRemotePresence(invalid, online = true).error)
        assertEquals(validationError, applyConnectionStatus(invalid, RemoteClient.ConnectionStatus.CONNECTED, "").error)
        val computerError = UserFacingError.fromMessage("Codex app-server is not running")
        assertEquals(computerError, applyRemotePresence(conversation.copy(error = computerError), online = true).error)
    }

    @Test
    fun stalePresenceCannotClearFailureAfterSocketClosed() {
        val disconnected = applyConnectionStatus(conversation, RemoteClient.ConnectionStatus.ERROR, "network suspended")
        assertEquals(disconnected, applyRemotePresence(disconnected, online = true))
    }

    @Test
    fun restoredActiveThreadKeepsDraftAndEnablesStopWithoutResendingAMessage() {
        val reconnected = applyConnectionStatus(conversation, RemoteClient.ConnectionStatus.CONNECTED, "")
        val response = JSONObject().put("thread", JSONObject()
            .put("id", thread.id).put("name", thread.title).put("cwd", thread.cwd)
            .put("turns", org.json.JSONArray().put(JSONObject()
                .put("id", "turn-1").put("status", "inProgress").put("items", org.json.JSONArray()))))
        val restored = applyThreadResponse(reconnected, "thread.resume", response)
        assertTrue(restored.canWriteThread)
        assertEquals("turn-1", restored.activeTurnId)
        assertEquals(conversation.draftText, restored.draftText)
        assertNull(restored.error)
    }
}
