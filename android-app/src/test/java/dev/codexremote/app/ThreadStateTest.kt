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

    @Test
    fun nullNameFallsBackToPreviewAndEmptySessionHasReadableTitle() {
        val value = JSONObject().put("id", thread.id).put("name", JSONObject.NULL).put("preview", "Fix session title")
        assertEquals("Fix session title", parseThread(value).title)
        value.put("preview", JSONObject.NULL)
        assertEquals("新会话", parseThread(value).title)
        value.put("name", "  ").put("preview", "")
        assertEquals("新会话", parseThread(value).title)
        value.put("name", "Explicit title").put("preview", "First prompt")
        assertEquals("Explicit title", parseThread(value).title)
    }

    @Test
    fun unnamedResumedSessionUsesFirstUserMessageFromHistory() {
        val root = response(false)
        root.getJSONObject("thread").put("name", JSONObject.NULL).put("preview", "")
        val state = applyThreadResponse(UiState(selectedThread = thread), "thread.resume", root)
        assertEquals("question", state.selectedThread?.title)
    }

    @Test
    fun newSessionAppearsInListWithTheSameReadableTitleAsItsHeader() {
        val root = JSONObject().put("thread", JSONObject().put("id", thread.id).put("name", JSONObject.NULL).put("preview", ""))
        val state = applyThreadResponse(UiState(), "thread.start", root)
        assertEquals("新会话", state.selectedThread?.title)
        assertEquals(state.selectedThread, state.threads.single())
    }

    @Test
    fun firstMessageUpdatesHeaderAndListWithoutReplacingAnExistingTitle() {
        val untitled = thread.copy(title = NEW_THREAD_TITLE)
        val initial = UiState(selectedThread = untitled, threads = listOf(untitled))
        val titled = applyFirstMessageTitle(initial, thread.id, "  Fix\n  the session title ")
        assertEquals("Fix the session title", titled.selectedThread?.title)
        assertEquals(titled.selectedThread?.title, titled.threads.single().title)
        assertEquals(titled, applyFirstMessageTitle(titled, thread.id, "Second message"))
        assertEquals(initial, applyFirstMessageTitle(initial, "other-thread", "Other message"))
        assertEquals(initial, applyFirstMessageTitle(initial, thread.id, ""))
    }

    @Test
    fun listRefreshUpdatesOpenSessionTitleAndPreservesLoadedHistory() {
        val initial = UiState(selectedThread = thread, messages = listOf(MessageUi("reply", "assistant", "history")), threadLoaded = true)
        val rows = JSONArray().put(JSONObject().put("id", thread.id).put("name", JSONObject.NULL).put("preview", "Updated preview"))
        val state = applyThreadList(initial, rows)
        assertEquals("Updated preview", state.selectedThread?.title)
        assertEquals("Updated preview", state.threads.single().title)
        assertEquals(initial.messages, state.messages)
        assertTrue(state.threadLoaded)
        rows.getJSONObject(0).put("preview", "")
        assertEquals("Updated preview", applyThreadList(state, rows).selectedThread?.title)
    }

    @Test
    fun nameEventUpdatesMatchingSessionWithoutChangingOtherSession() {
        val other = thread.copy(id = "thread-2", title = "Other title")
        val initial = UiState(selectedThread = thread, threads = listOf(thread, other))
        val state = applyThreadNameUpdate(initial, JSONObject().put("threadId", thread.id).put("threadName", "Named session"))
        assertEquals("Named session", state.selectedThread?.title)
        assertEquals(listOf("Named session", "Other title"), state.threads.map { it.title })
        val otherUpdated = applyThreadNameUpdate(state, JSONObject().put("threadId", other.id).put("threadName", "Other renamed"))
        assertEquals(state.selectedThread, otherUpdated.selectedThread)
        assertEquals("Other renamed", otherUpdated.threads.last().title)
        assertEquals(state, applyThreadNameUpdate(state, JSONObject().put("threadId", thread.id).put("threadName", JSONObject.NULL)))
    }
}
