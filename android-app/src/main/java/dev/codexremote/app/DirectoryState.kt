package dev.codexremote.app

import org.json.JSONObject

data class ComputerDirectory(val name: String, val path: String)

data class DirectoryBrowserState(
    val visible: Boolean = false,
    val path: String = "",
    val parent: String = "",
    val input: String = "",
    val entries: List<ComputerDirectory> = emptyList(),
    val nextOffset: Int? = null,
    val loading: Boolean = false,
    val creating: Boolean = false,
    val error: String? = null,
    val requestedPath: String = "",
    val requestedOffset: Int = 0,
)

internal fun applyDirectoryPage(state: DirectoryBrowserState, data: JSONObject, append: Boolean): DirectoryBrowserState {
    val path = data.getString("path")
    val rows = data.getJSONArray("entries")
    val entries = (0 until rows.length()).map {
        val row = rows.getJSONObject(it)
        ComputerDirectory(row.getString("name"), row.getString("path"))
    }
    val previous = if (append && path == state.path) state.entries else emptyList()
    return state.copy(
        path = path,
        parent = data.optString("parent", ""),
        input = path,
        entries = (previous + entries).distinctBy { it.path },
        nextOffset = if (data.isNull("nextOffset")) null else data.getInt("nextOffset"),
        loading = false,
        error = null,
    )
}

internal fun directoryFailure(message: String?): String = when {
    message?.contains("unsupported action") == true -> "电脑端尚不支持目录浏览，请先更新 codex-remote.exe 并重新运行。"
    else -> UserFacingError.fromMessage(message ?: "目录加载失败，请重试。").message
}
