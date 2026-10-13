package dev.codexremote.app

import org.json.JSONArray
import org.json.JSONObject

val UiState.canWriteThread: Boolean
    get() = threadLoaded && !threadReadOnly && !loadingThread

internal fun applyThreadResponse(state: UiState, action: String, root: JSONObject): UiState {
    val threadJson = root.optJSONObject("thread") ?: return state
    val thread = parseThread(threadJson)
    if (action == "thread.resume" && state.selectedThread?.id != thread.id) return state
    val readOnly = root.optBoolean("readOnly", false)
    val turns = threadJson.optJSONArray("turns")
    val lastTurn = turns?.optJSONObject(turns.length() - 1)
    val activeTurnId = if (!readOnly && lastTurn?.optString("status") == "inProgress") lastTurn.optString("id").ifBlank { null } else null
    return state.copy(
        selectedThread = thread,
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
    title = value.optString("name").ifBlank { value.optString("preview", "Untitled session") }.ifBlank { "Untitled session" },
    cwd = value.optString("cwd"),
    status = value.optJSONObject("status")?.optString("type") ?: "unknown",
    updatedAt = value.optLong("updatedAt"),
)

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
