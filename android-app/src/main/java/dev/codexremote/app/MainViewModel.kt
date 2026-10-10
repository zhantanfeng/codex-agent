package dev.codexremote.app

import android.app.Application
import android.net.Uri
import android.os.Looper
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import org.json.JSONArray
import org.json.JSONObject
import java.text.DateFormat
import java.util.Date
import java.io.File
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

data class ProjectUi(val id: String, val path: String)
data class ThreadUi(val id: String, val title: String, val cwd: String, val status: String, val updatedAt: Long)
data class MessageUi(val id: String, val role: String, val text: String, val detail: String = "", val images: List<MessageImage> = emptyList())
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
    val error: UserFacingError? = null,
    val retryCommand: RemoteCommand? = null,
    val threadLoaded: Boolean = false,
    val closingSession: Boolean = false,
    val draftText: String = "",
    val draftImages: List<DraftImage> = emptyList(),
    val preparingImages: Boolean = false,
    val sending: Boolean = false,
    val uploadProgress: String = "",
    val imagePreviews: Map<String, String> = emptyMap(),
)

class MainViewModel(application: Application) : AndroidViewModel(application), RemoteClient.Listener {
    private val remote = RemoteClient(application, this)
    private var preparationJob: Job? = null
    private var sendJob: Job? = null
    private val previewRequests = ConcurrentHashMap.newKeySet<String>()
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
        }.onFailure { update { current -> current.copy(error = UserFacingError.fromMessage(it.message ?: "Pairing failed")) } }
    }

    fun reconnect() = remote.connect()
    fun dismissError() = update { it.copy(error = null, retryCommand = null) }
    fun loadProjects() = remote.command("projects.list")
    fun loadThreads() = remote.command("threads.list")
    fun updateDraft(text: String) = update { it.copy(draftText = text) }

    fun addImages(uris: List<Uri>) {
        val threadId = state.value.selectedThread?.id ?: return
        if (uris.isEmpty() || sendJob?.isActive == true || preparationJob?.isActive == true) return
        val available = MAX_MESSAGE_IMAGES - state.value.draftImages.size
        if (available <= 0) {
            update { it.copy(error = UserFacingError("每条消息最多添加 4 张图片")) }
            return
        }
        preparationJob = viewModelScope.launch {
            update { it.copy(preparingImages = true, error = null) }
            try {
                for (uri in uris.take(available)) {
                    val image = withContext(Dispatchers.IO) { prepareImage(getApplication(), uri) }
                    if (state.value.selectedThread?.id != threadId) return@launch
                    update { it.copy(draftImages = it.draftImages + image) }
                }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                update { it.copy(error = UserFacingError(error.message ?: "图片处理失败，请重新选择")) }
            } finally {
                update { it.copy(preparingImages = false) }
            }
        }
    }

    fun removeImage(id: String) {
        if (state.value.sending) return
        update { it.copy(draftImages = it.draftImages.filterNot { image -> image.id == id }) }
    }

    fun loadImagePreview(threadId: String, id: String) {
        if (!id.matches(Regex("^[a-zA-Z0-9_-]{1,80}$"))) return
        if (state.value.imagePreviews.containsKey(id) || !previewRequests.add(id)) return
        viewModelScope.launch {
            try {
                val preview = withContext(Dispatchers.IO) {
                    val directory = File(getApplication<Application>().filesDir, "images").apply { mkdirs() }
                    val file = File(directory, "$id.thumb.jpg")
                    if (!file.exists()) {
                        val data = remote.request("image.thumbnail", JSONObject().put("threadId", threadId).put("id", id))
                        file.writeBytes(b64decode(data.getString("bytes")))
                    }
                    file.absolutePath
                }
                update { it.copy(imagePreviews = it.imagePreviews + (id to preview)) }
            } catch (error: Exception) {
                if (error is CancellationException) throw error
                // The message remains readable if its preview is temporarily unavailable.
            } finally {
                previewRequests.remove(id)
            }
        }
    }

    private fun cancelImageWork() {
        preparationJob?.cancel()
        sendJob?.cancel()
    }

    fun createThread(project: ProjectUi) {
        update { it.copy(error = null, retryCommand = null) }
        remote.command("thread.start", JSONObject().put("projectId", project.id))
    }

    fun openThread(thread: ThreadUi) {
        cancelImageWork()
        update {
            it.copy(
                selectedThread = thread,
                messages = emptyList(),
                approval = null,
                error = null,
                retryCommand = null,
                threadLoaded = false,
                draftText = "",
                draftImages = emptyList(),
                sending = false,
                preparingImages = false,
                uploadProgress = "",
            )
        }
        remote.command("thread.resume", JSONObject().put("threadId", thread.id))
    }

    fun retry() {
        val command = state.value.retryCommand
        if (command == null) {
            if (state.value.draftImages.isNotEmpty()) send(state.value.draftText)
            return
        }
        update { it.copy(error = null, retryCommand = null) }
        remote.command(command.action, command.data())
    }

    fun closeThread() {
        cancelImageWork()
        update {
            it.copy(
                selectedThread = null,
                messages = emptyList(),
                activeTurnId = null,
                approval = null,
                error = null,
                retryCommand = null,
                threadLoaded = false,
                closingSession = false,
                draftText = "",
                draftImages = emptyList(),
                sending = false,
                preparingImages = false,
                uploadProgress = "",
            )
        }
    }

    fun closePhoneSession() {
        val thread = state.value.selectedThread ?: return
        if (!state.value.threadLoaded || state.value.activeTurnId != null || state.value.closingSession || state.value.sending) return
        update { it.copy(closingSession = true, error = null, retryCommand = null) }
        remote.command("session.close", JSONObject().put("threadId", thread.id))
    }

    fun send(text: String, steer: Boolean = false) {
        val snapshot = state.value
        val thread = snapshot.selectedThread ?: return
        val trimmed = text.trim()
        val images = snapshot.draftImages
        if ((trimmed.isEmpty() && images.isEmpty()) || !snapshot.threadLoaded || sendJob?.isActive == true || preparationJob?.isActive == true) return
        val localId = "local-${UUID.randomUUID()}"
        sendJob = viewModelScope.launch {
            update { it.copy(sending = true, error = null, retryCommand = null) }
            var submitting = false
            try {
                val imageIds = withContext(Dispatchers.IO) {
                    images.mapIndexed { index, image ->
                        uploadImage(thread.id, image, remote::request) { percent ->
                            update { it.copy(uploadProgress = "上传图片 ${index + 1}/${images.size} · $percent%") }
                        }
                    }
                }
                val message = MessageUi(localId, "user", trimmed, images = images.map { MessageImage(it.id, it.name, it.previewPath) })
                update { it.copy(messages = it.messages + message, uploadProgress = "正在发送…") }
                submitting = true
                val data = JSONObject().put("threadId", thread.id).put("text", trimmed).put("imageIds", JSONArray(imageIds))
                val action = if (steer && snapshot.activeTurnId != null) "turn.steer" else "turn.start"
                if (action == "turn.steer") data.put("turnId", snapshot.activeTurnId)
                remote.request(action, data)
                update { it.copy(draftText = "", draftImages = emptyList()) }
            } catch (error: Exception) {
                if (error is CancellationException && !isActive) throw error
                val displayError = when {
                    error.message?.contains("unsupported action") == true && images.isNotEmpty() ->
                        UserFacingError("电脑端版本尚不支持图片，请更新电脑端程序后重试。")
                    submitting && (error is CancellationException || error.message?.contains("timed out") == true || error.message?.contains("relay is not connected") == true) ->
                        UserFacingError("发送结果尚未确认。请重新连接查看会话记录，再决定是否重发。", reconnectRequired = true)
                    else -> UserFacingError.fromMessage(error.message ?: "上传失败，图片和文字已保留，请重试发送。")
                }
                update {
                    it.copy(
                        error = displayError,
                        threadLoaded = if (displayError.reconnectRequired) false else it.threadLoaded,
                        messages = it.messages.filterNot { message -> message.id == localId },
                    )
                }
            } finally {
                update { it.copy(sending = false, uploadProgress = "") }
            }
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
        update {
            val disconnected = status == RemoteClient.ConnectionStatus.ERROR || status == RemoteClient.ConnectionStatus.DISCONNECTED
            it.copy(
                connection = status,
                connectionDetail = detail,
                error = if (disconnected) UserFacingError.connectionUnavailable() else it.error,
                closingSession = if (disconnected) false else it.closingSession,
                threadLoaded = if (disconnected) false else it.threadLoaded,
            )
        }
        if (status == RemoteClient.ConnectionStatus.CONNECTED && remote.paired) {
            refresh()
            state.value.selectedThread?.let {
                remote.command("thread.resume", JSONObject().put("threadId", it.id))
            }
        }
    }

    override fun onPaired() {
        update { it.copy(paired = true, pairingCode = null, error = null) }
        refresh()
    }

    override fun onResponse(command: RemoteCommand, payload: JSONObject) {
        val action = command.action
        if (!payload.optBoolean("ok")) {
            val error = UserFacingError.fromMessage(payload.optString("error", "Request failed"))
            update {
                if (action == "thread.resume" && it.selectedThread?.id != command.data().optString("threadId")) it else it.copy(
                    error = error,
                    retryCommand = command.takeIf { it.retryable && !error.reconnectRequired },
                    closingSession = if (action == "session.close") false else it.closingSession,
                    threadLoaded = if (error.reconnectRequired) false else it.threadLoaded,
                )
            }
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
                update {
                    it.copy(
                        threads = threads,
                        error = if (it.selectedThread == null && it.error?.reconnectRequired == true) null else it.error,
                    )
                }
            }
            "thread.start", "thread.resume" -> {
                val root = data as? JSONObject ?: return
                val threadJson = root.optJSONObject("thread") ?: return
                val thread = parseThread(threadJson)
                val history = parseHistory(threadJson.optJSONArray("turns"))
                update {
                    if (action == "thread.resume" && it.selectedThread?.id != thread.id) it else it.copy(
                        selectedThread = thread,
                        messages = history,
                        error = null,
                        retryCommand = null,
                        threadLoaded = true,
                    )
                }
            }
            "session.close" -> {
                update {
                    it.copy(
                        selectedThread = null,
                        messages = emptyList(),
                        activeTurnId = null,
                        approval = null,
                        error = null,
                        retryCommand = null,
                        threadLoaded = false,
                        closingSession = false,
                        draftText = "",
                        draftImages = emptyList(),
                    )
                }
                loadThreads()
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
            "host.status" -> if (payload.optBoolean("online")) {
                refresh()
                state.value.selectedThread?.takeIf { !state.value.threadLoaded }?.let {
                    remote.command("thread.resume", JSONObject().put("threadId", it.id))
                }
            }
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
        if (params.optString("threadId").isNotEmpty() && params.optString("threadId") != state.value.selectedThread?.id) {
            if (eventId > 0) update { it.copy(lastEventId = eventId) }
            return
        }
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
            "error" -> update {
                it.copy(error = UserFacingError.fromMessage(params.optJSONObject("error")?.optString("message") ?: "Codex error"))
            }
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
            "userMessage" -> {
                val message = parseUserMessage(item)
                update { current ->
                    val messages = current.messages.toMutableList()
                    val index = messages.indexOfFirst {
                        it.id == message.id || (it.id.startsWith("local-") && it.text == message.text && it.images.map { image -> image.id } == message.images.map { image -> image.id })
                    }
                    if (index >= 0) messages[index] = message else messages += message
                    current.copy(messages = messages)
                }
            }
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
                        result += parseUserMessage(item)
                    }
                    "agentMessage" -> result += MessageUi(item.optString("id"), "assistant", item.optString("text"))
                    "commandExecution" -> result += MessageUi(item.optString("id"), "command", item.optString("aggregatedOutput"), item.optString("command"))
                }
            }
        }
        return result
    }

    private fun update(block: (UiState) -> UiState) {
        if (Looper.myLooper() == Looper.getMainLooper()) state.value = block(state.value)
        else android.os.Handler(Looper.getMainLooper()).post { state.value = block(state.value) }
    }

    override fun onCleared() {
        remote.disconnect()
        super.onCleared()
    }
}
