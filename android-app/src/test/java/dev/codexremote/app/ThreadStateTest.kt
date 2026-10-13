package dev.codexremote.app

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ThreadStateTest {
    private val thread = ThreadUi("thread-1", "title", "D:/project", "idle", 0)

    private fun response(readOnly: Boolean) = JSONObject().put("readOnly", readOnly).put("thread", JSONObject()
        .put("id", thread.id).put("cwd", thread.cwd).put("name", thread.title)
        .put("turns", JSONArray().put(JSONObject().put("id", "turn-1").put("status", "completed")
            .put("items", JSONArray()
                .put(JSONObject().put("id", "user-1").put("type", "userMessage")
                    .put("content", JSONArray().put(JSONObject().put("type", "text").put("text", "question"))))
                .put(JSONObject().put("id", "reply-1").put("type", "agentMessage").put("text", "answer"))))))

    @Test
    fun occupiedSessionShowsHistoryAndDisablesWriteAccess() {
        val state = applyThreadResponse(UiState(selectedThread = thread, loadingThread = true, activeTurnId = "stale"), "thread.resume", response(true))

        assertEquals(listOf("question", "answer"), state.messages.map { it.text })
        assertTrue(state.threadLoaded)
        assertTrue(state.threadReadOnly)
        assertFalse(state.loadingThread)
        assertFalse(state.canWriteThread)
        assertNull(state.activeTurnId)
        assertNull(state.error)
    }

    @Test
    fun retryAfterOtherClientClosesRestoresInputAndKeepsDraft() {
        val occupied = applyThreadResponse(UiState(selectedThread = thread, draftText = "draft"), "thread.resume", response(true))
        val restored = applyThreadResponse(occupied.copy(loadingThread = true), "thread.resume", response(false))

        assertFalse(restored.threadReadOnly)
        assertTrue(restored.canWriteThread)
        assertEquals("draft", restored.draftText)
        assertEquals(2, restored.messages.size)
    }

    @Test
    fun delayedReadOnlyResponseDoesNotReopenClosedSession() {
        val closed = UiState()

        assertEquals(closed, applyThreadResponse(closed, "thread.resume", response(true)))
    }

    @Test
    fun retryInProgressDoesNotEnableSendingBeforeResumeFinishes() {
        assertFalse(UiState(threadLoaded = true, loadingThread = true).canWriteThread)
    }
}
