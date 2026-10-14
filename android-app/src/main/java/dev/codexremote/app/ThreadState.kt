package dev.codexremote.app

import org.json.JSONArray
import org.json.JSONObject

internal const val NEW_THREAD_TITLE = "新会话"

val UiState.canWriteThread: Boolean
    get() = threadLoaded && !threadReadOnly && !loadingThread

internal fun applyThreadResponse(state: UiState, action: String, root: JSONObject): UiState {
    val threadJson = root.optJSONObject("thread") ?: return state
    val parsedThread = parseThread(threadJson)
    val thread = keepKnownTitle(state.selectedThread?.takeIf { it.id == parsedThread.id }, parsedThread)
    if (action == "thread.resume" && state.selectedThread?.id != thread.id) return state
    val readOnly = root.optBoolean("readOnly", false)
    val turns = threadJson.optJSONArray("turns")
    val lastTurn = turns?.optJSONObject(turns.length() - 1)
    val activeTurnId = if (!readOnly && lastTurn?.optString("status") == "inProgress") lastTurn.optString("id").ifBlank { null } else null
    return state.copy(
        selectedThread = thread,
        threads = if (state.threads.none { it.id == thread.id }) listOf(thread) + state.threads
            else state.threads.map { if (it.id == thread.id) thread else it },
        messages = parseHistory(turns),
        error = null,
        retryCommand = null,
        threadLoaded = true,
        threadReadOnly = readOnly,
        loadingThread = false,
        threadLoadProgress = 100,
        activeTurnId = activeTurnId,
        approval = if (readOnly) null else state.approval,
    )
}

internal fun parseThread(value: JSONObject): ThreadUi = ThreadUi(
    id = value.getString("id"),
    title = value.nonBlankString("name") ?: value.nonBlankString("preview")
        ?: parseHistory(value.optJSONArray("turns")).firstOrNull { it.role == "user" && it.text.isNotBlank() }?.text?.asThreadTitle()
        ?: NEW_THREAD_TITLE,
    cwd = value.optString("cwd"),
    status = value.optJSONObject("status")?.optString("type") ?: "unknown",
    updatedAt = value.optLong("updatedAt"),
)

internal fun applyThreadList(state: UiState, rows: JSONArray): UiState {
    val known = state.threads.associateBy { it.id }.toMutableMap()
    state.selectedThread?.let { known[it.id] = it }
    val threads = (0 until rows.length()).map {
        val thread = parseThread(rows.getJSONObject(it))
        keepKnownTitle(known[thread.id], thread)
    }
    return state.copy(
        threads = threads,
        selectedThread = state.selectedThread?.let { selected -> threads.firstOrNull { it.id == selected.id } ?: selected },
    )
}

internal fun applyThreadNameUpdate(state: UiState, params: JSONObject): UiState {
    val id = params.nonBlankString("threadId") ?: return state
    val title = params.nonBlankString("threadName") ?: return state
    return state.copy(
        selectedThread = state.selectedThread?.let { if (it.id == id) it.copy(title = title) else it },
        threads = state.threads.map { if (it.id == id) it.copy(title = title) else it },
    )
}

internal fun applyFirstMessageTitle(state: UiState, threadId: String, text: String): UiState {
    val selected = state.selectedThread ?: return state
    if (selected.id != threadId || selected.title != NEW_THREAD_TITLE || text.isBlank()) return state
    val titled = selected.copy(title = text.asThreadTitle())
    return state.copy(selectedThread = titled, threads = state.threads.map { if (it.id == threadId) titled else it })
}

private fun keepKnownTitle(known: ThreadUi?, updated: ThreadUi): ThreadUi =
    if (updated.title == NEW_THREAD_TITLE && known != null) updated.copy(title = known.title) else updated

// JSON null must not become the literal string "null" on Android.
private fun JSONObject.nonBlankString(key: String): String? = (opt(key) as? String)?.trim()?.takeIf { it.isNotEmpty() }

private fun String.asThreadTitle(): String = trim().replace(Regex("\\s+"), " ").take(80)

private fun parseHistory(turns: JSONArray?): List<MessageUi> {
    if (turns == null) return emptyList()
    val result = mutableListOf<MessageUi>()
    for (turnIndex in 0 until turns.length()) {
        val items = turns.getJSONObject(turnIndex).optJSONArray("items") ?: continue
        for (itemIndex in 0 until items.length()) {
            val item = items.getJSONObject(itemIndex)
            when (item.optString("type")) {
                "userMessage" -> result += parseUserMessage(item)
                "agentMessage" -> result += MessageUi(item.optString("id"), "assistant", item.optString("text"))
                "commandExecution" -> result += MessageUi(item.optString("id"), "command", item.optString("aggregatedOutput"), item.optString("command"))
            }
        }
    }
    return result
}
