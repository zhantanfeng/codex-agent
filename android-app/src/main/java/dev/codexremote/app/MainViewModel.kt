package dev.codexremote.app

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import org.json.JSONArray
import org.json.JSONObject
import java.text.DateFormat
import java.util.Date

data class ProjectUi(val id: String, val path: String)
data class ThreadUi(val id: String, val title: String, val cwd: String, val status: String, val updatedAt: Long)
data class MessageUi(val id: String, val role: String, val text: String, val detail: String = "")
data class ApprovalUi(val requestId: String, val title: String, val detail: String)

data class UiState(
    val setupRequired: Boolean = true,
    val connection: RemoteClient.ConnectionStatus = RemoteClient.ConnectionStatus.DISCONNECTED,
    val connectionDetail: String = "",
    val paired: Boolean = false,
    val projects: List<ProjectUi> = emptyList(),
    val threads: List<ThreadUi> = emptyList(),
    val selectedThread: ThreadUi? = null,
    val messages: List<MessageUi> = emptyList(),
    val activeTurnId: String? = null,
    val approval: ApprovalUi? = null,
    val pairingCode: String? = null,
    val lastEventId: Long = 0,
    val error: String? = null,
)

class MainViewModel(application: Application) : AndroidViewModel(application), RemoteClient.Listener {
    private val remote = RemoteClient(application, this)
    var state = androidx.compose.runtime.mutableStateOf(
        UiState(setupRequired = !remote.configured(), paired = remote.paired),
    )
        private set

    init {
        if (remote.configured()) remote.connect()
    }

    fun configure(raw: String) {
        runCatching {
            remote.configure(raw)
            update { it.copy(setupRequired = false, paired = false, error = null) }
            remote.connect()
        }.onFailure { update { current -> current.copy(error = it.message) } }
    }

    fun reconnect() = remote.connect()
    fun loadProjects() = remote.command("projects.list")
    fun loadThreads() = remote.command("threads.list")

    fun createThread(project: ProjectUi) {
        remote.command("thread.start", JSONObject().put("projectId", project.id))
    }

    fun openThread(thread: ThreadUi) {
        update { it.copy(selectedThread = thread, messages = emptyList(), approval = null) }
        remote.command("thread.resume", JSONObject().put("threadId", thread.id))
    }

    fun closeThread() = update { it.copy(selectedThread = null, messages = emptyList(), activeTurnId = null, approval = null) }

    fun send(text: String, steer: Boolean = false) {
        val thread = state.value.selectedThread ?: return
        val trimmed = text.trim()
        if (trimmed.isEmpty()) return
        update { it.copy(messages = it.messages + MessageUi("local-${System.nanoTime()}", "user", trimmed)) }
        if (steer && state.value.activeTurnId != null) {
            remote.command(
                "turn.steer",
                JSONObject().put("threadId", thread.id).put("turnId", state.value.activeTurnId).put("text", trimmed),
            )
        } else {
            remote.command("turn.start", JSONObject().put("threadId", thread.id).put("text", trimmed))
        }
    }

    fun interrupt() {
        val threadId = state.value.selectedThread?.id ?: return
        val turnId = state.value.activeTurnId ?: return
        remote.command("turn.interrupt", JSONObject().put("threadId", threadId).put("turnId", turnId))
    }

    fun approve(decision: String) {
        val approval = state.value.approval ?: return
        remote.command("approval.respond", JSONObject().put("requestId", approval.requestId).put("decision", decision))
        update { it.copy(approval = null) }
    }

    override fun onConnection(status: RemoteClient.ConnectionStatus, detail: String) {
        update { it.copy(connection = status, connectionDetail = detail) }
        if (status == RemoteClient.ConnectionStatus.CONNECTED && remote.paired) refresh()
    }

    override fun onPaired() {
        update { it.copy(paired = true, pairingCode = null, error = null) }
        refresh()
    }

    override fun onResponse(action: String, payload: JSONObject) {
        if (!payload.optBoolean("ok")) {
            update { it.copy(error = payload.optString("error", "Request failed")) }
            return
        }
        val data = payload.opt("data")
        when (action) {
            "projects.list" -> {
                val objectValue = data as? JSONObject ?: JSONObject()
                val projects = objectValue.keys().asSequence().map { ProjectUi(it, objectValue.getString(it)) }.sortedBy { it.id }.toList()
                update { it.copy(projects = projects) }
            }
            "threads.list" -> {
                val rows = (data as? JSONObject)?.optJSONArray("data") ?: JSONArray()
                val threads = (0 until rows.length()).map { parseThread(rows.getJSONObject(it)) }
                update { it.copy(threads = threads) }
            }
            "thread.start", "thread.resume" -> {
                val root = data as? JSONObject ?: return
                val threadJson = root.optJSONObject("thread") ?: return
                val thread = parseThread(threadJson)
                val history = parseHistory(threadJson.optJSONArray("turns"))
                update { it.copy(selectedThread = thread, messages = history, error = null) }
            }
            "events.sync" -> {
                val synced = data as? JSONObject ?: return
                val events = synced.optJSONArray("events") ?: JSONArray()
                for (index in 0 until events.length()) handleCodexEvent(events.getJSONObject(index))
                val approvals = synced.optJSONArray("approvals") ?: JSONArray()
                for (index in 0 until approvals.length()) handleApproval(approvals.getJSONObject(index))
            }
        }
    }

    override fun onEvent(kind: String, payload: JSONObject) {
        when (kind) {
            "pair.pending" -> update { it.copy(pairingCode = payload.optString("verificationCode")) }
            "codex.event" -> handleCodexEvent(payload)
            "codex.request" -> handleApproval(payload)
        }
    }

    private fun refresh() {
        loadProjects()
        loadThreads()
        remote.command("events.sync", JSONObject().put("after", state.value.lastEventId))
    }

    private fun handleCodexEvent(event: JSONObject) {
        val eventId = event.optLong("eventId")
        if (eventId != 0L && eventId <= state.value.lastEventId) return
        val method = event.optString("method")
        val params = event.optJSONObject("params") ?: JSONObject()
        when (method) {
            "turn/started" -> update { it.copy(activeTurnId = params.optJSONObject("turn")?.optString("id")) }
            "turn/completed" -> update { it.copy(activeTurnId = null) }
            "item/agentMessage/delta" -> appendDelta(params.optString("itemId"), params.optString("delta"))
            "item/commandExecution/outputDelta" -> appendDelta(params.optString("itemId"), params.optString("delta"), "command")
            "item/fileChange/patchUpdated" -> upsertMessage(
                params.optString("itemId"),
                "file",
                params.optJSONArray("changes")?.toString(2) ?: "Files changed",
                "Patch updated",
            )
            "item/started", "item/completed" -> upsertItem(params.optJSONObject("item"))
            "turn/diff/updated" -> upsertMessage("diff-${params.optString("turnId")}", "diff", params.optString("diff"), "Workspace diff")
            "error" -> update { it.copy(error = params.optJSONObject("error")?.optString("message") ?: "Codex error") }
        }
        if (eventId > 0) update { it.copy(lastEventId = eventId) }
    }

    private fun appendDelta(itemId: String, delta: String, role: String = "assistant") {
        val messages = state.value.messages.toMutableList()
        val index = messages.indexOfFirst { it.id == itemId }
        if (index >= 0) messages[index] = messages[index].copy(text = messages[index].text + delta)
        else messages += MessageUi(itemId, role, delta)
        update { it.copy(messages = messages) }
    }

    private fun handleApproval(payload: JSONObject) {
        val params = payload.optJSONObject("params") ?: JSONObject()
        val method = payload.optString("method")
        val title = if (method.contains("fileChange") || method.contains("Patch")) "Approve file changes" else "Approve command"
        val detail = params.optString("command", params.optString("reason", method))
        update { it.copy(approval = ApprovalUi(payload.getString("requestId"), title, detail)) }
    }

    private fun upsertItem(item: JSONObject?) {
        if (item == null) return
        val id = item.optString("id", "item-${System.nanoTime()}")
        when (item.optString("type")) {
            "agentMessage" -> upsertMessage(id, "assistant", item.optString("text"))
            "commandExecution" -> upsertMessage(id, "command", item.optString("aggregatedOutput"), item.optString("command"))
            "fileChange" -> upsertMessage(id, "file", item.optJSONArray("changes")?.toString(2) ?: "Files changed", item.optString("status"))
        }
    }

    private fun upsertMessage(id: String, role: String, text: String, detail: String = "") {
        val messages = state.value.messages.toMutableList()
        val index = messages.indexOfFirst { it.id == id }
        val value = MessageUi(id, role, text, detail)
        if (index >= 0) messages[index] = value else messages += value
        update { it.copy(messages = messages) }
    }

    private fun parseThread(value: JSONObject): ThreadUi {
        val status = value.optJSONObject("status")?.optString("type") ?: "unknown"
        return ThreadUi(
            id = value.getString("id"),
            title = value.optString("name").ifBlank { value.optString("preview", "Untitled session") }.ifBlank { "Untitled session" },
            cwd = value.optString("cwd"),
            status = status,
            updatedAt = value.optLong("updatedAt"),
        )
    }

    private fun parseHistory(turns: JSONArray?): List<MessageUi> {
        if (turns == null) return emptyList()
        val result = mutableListOf<MessageUi>()
        for (turnIndex in 0 until turns.length()) {
            val items = turns.getJSONObject(turnIndex).optJSONArray("items") ?: continue
            for (itemIndex in 0 until items.length()) {
                val item = items.getJSONObject(itemIndex)
                when (item.optString("type")) {
                    "userMessage" -> {
                        val content = item.optJSONArray("content")
                        val text = (0 until (content?.length() ?: 0)).mapNotNull { content?.optJSONObject(it)?.optString("text") }.joinToString("\n")
                        result += MessageUi(item.optString("id"), "user", text)
                    }
                    "agentMessage" -> result += MessageUi(item.optString("id"), "assistant", item.optString("text"))
                    "commandExecution" -> result += MessageUi(item.optString("id"), "command", item.optString("aggregatedOutput"), item.optString("command"))
                }
            }
        }
        return result
    }

    private fun update(block: (UiState) -> UiState) {
        android.os.Handler(android.os.Looper.getMainLooper()).post { state.value = block(state.value) }
    }

    override fun onCleared() {
        remote.disconnect()
        super.onCleared()
    }
}
