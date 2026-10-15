package dev.codexremote.app

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun DirectoryBrowserScreen(state: UiState, model: MainViewModel) {
    val browser = state.directories
    val busy = browser.loading || browser.creating
    val connected = state.connection == RemoteClient.ConnectionStatus.CONNECTED
    val listState = rememberLazyListState()
    LaunchedEffect(browser.path) { listState.scrollToItem(0) }
    BackHandler { model.closeDirectories() }
    Scaffold(
        contentWindowInsets = WindowInsets.navigationBars,
        topBar = {
            TopAppBar(
                title = { Text("选择电脑目录") },
                navigationIcon = { IconButton(onClick = model::closeDirectories) { Icon(Icons.Default.ArrowBack, "返回主界面") } },
                actions = {
                    IconButton(onClick = { model.browseDirectory(browser.path) }, enabled = !busy && connected) {
                        Icon(Icons.Default.Refresh, "刷新目录")
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding).consumeWindowInsets(padding).imePadding()) {
            Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
                OutlinedTextField(
                    value = browser.input,
                    onValueChange = model::updateDirectoryInput,
                    label = { Text("电脑路径") },
                    placeholder = { Text("D:\\projects") },
                    singleLine = true,
                    enabled = !busy,
                    modifier = Modifier.weight(1f),
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                    keyboardActions = KeyboardActions(onGo = { if (!busy && connected) model.browseDirectory(browser.input.trim()) }),
                )
                TextButton(onClick = { model.browseDirectory(browser.input.trim()) }, enabled = !busy && connected) { Text("前往") }
            }
            Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                TextButton(onClick = { model.browseDirectory("") }, enabled = !busy && connected) { Text("磁盘列表") }
                if (browser.path.isNotBlank()) TextButton(onClick = { model.browseDirectory(browser.parent) }, enabled = !busy && connected) {
                    Text("返回上级")
                }
                Spacer(Modifier.weight(1f))
                if (busy) CircularProgressIndicator(Modifier.size(20.dp))
            }
            Text(browser.path.ifBlank { "电脑上的磁盘" }, style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(horizontal = 16.dp, vertical = 6.dp))
            if (busy) LinearProgressIndicator(Modifier.fillMaxWidth())
            if (!connected) TextButton(onClick = model::reconnect) { Text("电脑连接已断开，点击重新连接") }
            browser.error?.let { error ->
                Column(Modifier.padding(16.dp)) {
                    Text(error, color = MaterialTheme.colorScheme.error)
                    TextButton(onClick = model::retryDirectory, enabled = !busy && connected) { Text("重新加载目录") }
                }
            }
            LazyColumn(state = listState, modifier = Modifier.weight(1f).fillMaxWidth(), contentPadding = PaddingValues(bottom = 12.dp)) {
                items(browser.entries, key = { it.path }) { entry ->
                    Row(
                        Modifier.fillMaxWidth().clickable(enabled = !busy && connected) { model.browseDirectory(entry.path) }.padding(16.dp),
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Icon(Icons.Default.Folder, contentDescription = null, tint = MaterialTheme.colorScheme.primary)
                        Text(entry.name, modifier = Modifier.weight(1f))
                    }
                    HorizontalDivider()
                }
                if (browser.entries.isEmpty() && !busy && browser.error == null) item {
                    Text(if (browser.path.isBlank()) "未发现可用磁盘" else "此目录没有子文件夹，可以直接在这里新建会话。", modifier = Modifier.padding(20.dp))
                }
                browser.nextOffset?.let { offset -> item {
                    TextButton(onClick = { model.browseDirectory(browser.path, offset) }, enabled = !busy && connected, modifier = Modifier.fillMaxWidth()) { Text("加载更多文件夹") }
                } }
            }
            Button(
                onClick = model::createThreadInDirectory,
                enabled = connected && browser.path.isNotBlank() && !busy && browser.error == null,
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
            ) {
                Icon(Icons.Default.Add, contentDescription = null)
                Spacer(Modifier.width(8.dp))
                Text(if (browser.creating) "正在创建会话…" else "在此目录新建会话")
            }
            Text("以电脑当前账户权限访问；所选目录会自动保存到项目列表。", style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 12.dp))
        }
    }
}
