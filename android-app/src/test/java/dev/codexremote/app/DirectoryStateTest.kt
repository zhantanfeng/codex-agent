package dev.codexremote.app

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DirectoryStateTest {
    @Test
    fun creationUsesOnlyTheSuccessfullyLoadedFolder() {
        val loaded = DirectoryBrowserState(path = "D:\\projects", input = "D:\\projects")
        assertTrue(loaded.canCreateSession)
        assertFalse(loaded.copy(input = "D:\\other").canCreateSession)
        assertFalse(loaded.copy(loading = true).canCreateSession)
        assertFalse(loaded.copy(creating = true).canCreateSession)
        assertFalse(loaded.copy(error = "目录不存在").canCreateSession)
        assertFalse(DirectoryBrowserState().canCreateSession)
    }

    private fun page(path: String, names: List<String>, next: Int? = null) = JSONObject()
        .put("path", path).put("parent", if (path == "D:\\") "" else "D:\\")
        .put("entries", JSONArray(names.map { JSONObject().put("name", it).put("path", "$path\\$it") }))
        .put("nextOffset", next ?: JSONObject.NULL)

    @Test
    fun enteringFolderReplacesPreviousPageAndUpdatesTheActualPath() {
        val state = applyDirectoryPage(DirectoryBrowserState(visible = true, loading = true, input = "typed path"), page("D:\\projects", listOf("中文目录", "demo"), 2), false)
        assertEquals("D:\\projects", state.path)
        assertEquals(state.path, state.input)
        assertEquals(listOf("中文目录", "demo"), state.entries.map { it.name })
        assertEquals(2, state.nextOffset)
        assertFalse(state.loading)
        val empty = applyDirectoryPage(state, page("D:\\empty", emptyList()), false)
        assertEquals(emptyList<ComputerDirectory>(), empty.entries)
        assertNull(empty.nextOffset)
    }

    @Test
    fun loadingMoreAppendsAndDeduplicatesButDoesNotMixDirectories() {
        val first = applyDirectoryPage(DirectoryBrowserState(), page("D:\\", listOf("a", "b"), 2), false)
        val next = applyDirectoryPage(first, page("D:\\", listOf("b", "c")), true)
        assertEquals(listOf("a", "b", "c"), next.entries.map { it.name })
        assertNull(next.nextOffset)
        val other = applyDirectoryPage(next, page("C:\\", listOf("Windows")), true)
        assertEquals(listOf("Windows"), other.entries.map { it.name })
    }

    @Test
    fun rootsAndOldAgentFailuresHaveUsableDisplayValues() {
        val roots = applyDirectoryPage(DirectoryBrowserState(error = "old failure"), page("", listOf("C:\\", "D:\\")), false)
        assertEquals("", roots.path)
        assertEquals("", roots.input)
        assertNull(roots.error)
        assertEquals("电脑端尚不支持目录浏览，请先更新 codex-remote.exe 并重新运行。", directoryFailure("unsupported action directories.list"))
        assertEquals(UserFacingError.connectionUnavailable().message, directoryFailure("relay is not connected"))
        assertEquals("目录不存在", directoryFailure("目录不存在"))
    }
}
