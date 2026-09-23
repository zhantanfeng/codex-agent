package dev.codexremote.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Computer
import androidx.compose.material.icons.filled.QrCodeScanner
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Send
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import java.text.DateFormat
import java.util.Date

private val Ink = Color(0xFF202321)
private val Canvas = Color(0xFFF7F8F5)
private val Paper = Color(0xFFFFFFFF)
private val Moss = Color(0xFF216E4E)
private val Coral = Color(0xFFC94B3C)
private val Muted = Color(0xFF687069)
private val Line = Color(0xFFDDE1DB)

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            MaterialTheme(
                colorScheme = MaterialTheme.colorScheme.copy(
                    primary = Moss,
                    secondary = Coral,
                    background = Canvas,
                    surface = Paper,
                    onSurface = Ink,
                ),
            ) {
                val model: MainViewModel = viewModel()
                App(model)
            }
        }
    }
}

@Composable
private fun App(model: MainViewModel) {
    val state by model.state
    Surface(modifier = Modifier.fillMaxSize(), color = Canvas) {
        when {
            state.setupRequired || !state.paired -> SetupScreen(state, model)
            state.selectedThread != null -> ConversationScreen(state, model)
            else -> HomeScreen(state, model)
        }
        state.error?.let { ErrorBanner(it) }
        state.approval?.let { ApprovalDialog(it, model) }
        state.pairingCode?.let { PairingCodeDialog(it) }
    }
}

@Composable
private fun PairingCodeDialog(code: String) {
    AlertDialog(
        onDismissRequest = {},
        title = { Text("Confirm on Windows") },
        text = {
            Column {
                Text("Enter this code in the Windows pairing prompt. Only approve when both screens match.")
                Text(code, style = MaterialTheme.typography.displaySmall, fontWeight = FontWeight.Bold, modifier = Modifier.padding(top = 16.dp))
            }
        },
        confirmButton = {},
    )
}

@Composable
private fun SetupScreen(state: UiState, model: MainViewModel) {
    var pairing by remember { mutableStateOf("") }
    val scanner = rememberLauncherForActivityResult(ScanContract()) { result ->
        result.contents?.let(model::configure)
    }
    Column(
        modifier = Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding().padding(24.dp),
        verticalArrangement = Arrangement.Center,
    ) {
        Icon(Icons.Default.Computer, contentDescription = null, tint = Moss, modifier = Modifier.size(42.dp))
        Spacer(Modifier.height(18.dp))
        Text("Codex Remote", style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.SemiBold)
        Text(
            if (state.connection == RemoteClient.ConnectionStatus.CONNECTING) "Connecting to your Windows host" else "Pair this phone with the Windows Agent",
            color = Muted,
            modifier = Modifier.padding(top = 8.dp, bottom = 24.dp),
        )
        Button(
            onClick = {
                scanner.launch(ScanOptions().setDesiredBarcodeFormats(ScanOptions.QR_CODE).setPrompt("Scan the code shown by codex-remote pair"))
            },
            modifier = Modifier.fillMaxWidth().height(50.dp),
        ) {
            Icon(Icons.Default.QrCodeScanner, contentDescription = null)
            Spacer(Modifier.width(10.dp))
            Text("Scan pairing code")
        }
        Text("or paste the pairing payload", color = Muted, style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(vertical = 16.dp))
        OutlinedTextField(
            value = pairing,
            onValueChange = { pairing = it },
            modifier = Modifier.fillMaxWidth(),
            minLines = 3,
            label = { Text("Pairing payload") },
        )
        OutlinedButton(
            onClick = { model.configure(pairing) },
            enabled = pairing.isNotBlank(),
            modifier = Modifier.fillMaxWidth().padding(top = 12.dp),
        ) { Text("Pair manually") }
        if (state.connection == RemoteClient.ConnectionStatus.CONNECTING) {
            CircularProgressIndicator(modifier = Modifier.size(24.dp).align(Alignment.CenterHorizontally).padding(top = 18.dp))
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun HomeScreen(state: UiState, model: MainViewModel) {
    Scaffold(
        containerColor = Canvas,
        contentWindowInsets = WindowInsets(0),
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("Codex Remote", fontWeight = FontWeight.SemiBold)
                        ConnectionLabel(state.connection)
                    }
                },
                actions = { IconButton(onClick = model::reconnect) { Icon(Icons.Default.Refresh, "Reconnect") } },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = Canvas),
                modifier = Modifier.statusBarsPadding(),
            )
        },
    ) { padding ->
        LazyColumn(modifier = Modifier.fillMaxSize().padding(padding), contentPadding = PaddingValues(bottom = 28.dp)) {
            item { SectionHeader("Projects", "Start a new Codex session") }
            items(state.projects, key = { it.id }) { project -> ProjectRow(project) { model.createThread(project) } }
            item { SectionHeader("Recent sessions", "Running on your Windows computer") }
            items(state.threads, key = { it.id }) { thread -> ThreadRow(thread) { model.openThread(thread) } }
            if (state.projects.isEmpty() && state.threads.isEmpty()) {
                item { EmptyState() }
            }
        }
    }
}

@Composable
private fun SectionHeader(title: String, caption: String) {
    Column(Modifier.fillMaxWidth().padding(start = 20.dp, end = 20.dp, top = 22.dp, bottom = 8.dp)) {
        Text(title, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
        Text(caption, style = MaterialTheme.typography.bodySmall, color = Muted)
    }
}

@Composable
private fun ProjectRow(project: ProjectUi, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(38.dp).background(Color(0xFFE7F1EB), RoundedCornerShape(6.dp)), contentAlignment = Alignment.Center) {
            Icon(Icons.Default.Add, contentDescription = null, tint = Moss)
        }
        Column(Modifier.weight(1f).padding(start = 12.dp)) {
            Text(project.id, fontWeight = FontWeight.Medium)
            Text(project.path, style = MaterialTheme.typography.bodySmall, color = Muted, maxLines = 1)
        }
    }
    HorizontalDivider(color = Line, modifier = Modifier.padding(start = 70.dp))
}

@Composable
private fun ThreadRow(thread: ThreadUi, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 20.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        StatusDot(thread.status == "active")
        Column(Modifier.weight(1f).padding(horizontal = 12.dp)) {
            Text(thread.title, fontWeight = FontWeight.Medium, maxLines = 2)
            Text(thread.cwd, style = MaterialTheme.typography.bodySmall, color = Muted, maxLines = 1)
        }
        Text(DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(thread.updatedAt * 1000)), style = MaterialTheme.typography.labelSmall, color = Muted)
    }
    HorizontalDivider(color = Line, modifier = Modifier.padding(start = 44.dp))
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun ConversationScreen(state: UiState, model: MainViewModel) {
    var input by remember { mutableStateOf("") }
    val thread = state.selectedThread ?: return
    Scaffold(
        containerColor = Canvas,
        contentWindowInsets = WindowInsets(0),
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = model::closeThread) { Icon(Icons.Default.ArrowBack, "Back") } },
                title = {
                    Column {
                        Text(thread.title, style = MaterialTheme.typography.titleMedium, maxLines = 1)
                        Text(thread.cwd, style = MaterialTheme.typography.labelSmall, color = Muted, maxLines = 1)
                    }
                },
                actions = {
                    if (state.activeTurnId != null) IconButton(onClick = model::interrupt) { Icon(Icons.Default.Stop, "Stop", tint = Coral) }
                },
                colors = TopAppBarDefaults.topAppBarColors(containerColor = Canvas),
                modifier = Modifier.statusBarsPadding(),
            )
        },
        bottomBar = {
            Row(
                Modifier.fillMaxWidth().background(Paper).navigationBarsPadding().imePadding().padding(12.dp),
                verticalAlignment = Alignment.Bottom,
            ) {
                OutlinedTextField(
                    value = input,
                    onValueChange = { input = it },
                    modifier = Modifier.weight(1f),
                    placeholder = { Text(if (state.activeTurnId == null) "Message Codex" else "Queue or steer Codex") },
                    maxLines = 5,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                    keyboardActions = KeyboardActions(onSend = { model.send(input); input = "" }),
                )
                Spacer(Modifier.width(8.dp))
                if (state.activeTurnId != null) {
                    OutlinedButton(
                        onClick = { model.send(input, steer = true); input = "" },
                        enabled = input.isNotBlank(),
                        contentPadding = PaddingValues(horizontal = 12.dp),
                        modifier = Modifier.height(56.dp),
                    ) { Text("Steer") }
                    Spacer(Modifier.width(8.dp))
                }
                FilledIconButton(onClick = { model.send(input); input = "" }, enabled = input.isNotBlank()) {
                    Icon(Icons.Default.Send, "Send")
                }
            }
        },
    ) { padding ->
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(padding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            items(state.messages, key = { it.id }) { message -> MessageBlock(message) }
            if (state.activeTurnId != null) item { Text("Codex is working...", color = Moss, style = MaterialTheme.typography.labelMedium) }
        }
    }
}

@Composable
private fun MessageBlock(message: MessageUi) {
    val user = message.role == "user"
    val technical = message.role in setOf("command", "file", "diff")
    Column(
        modifier = Modifier.fillMaxWidth(if (user) 0.9f else 1f)
            .background(if (user) Color(0xFFE7F1EB) else if (technical) Color(0xFFF0F1EE) else Paper, RoundedCornerShape(8.dp))
            .padding(14.dp),
    ) {
        if (message.detail.isNotBlank()) Text(message.detail, style = MaterialTheme.typography.labelMedium, color = if (technical) Coral else Muted)
        Text(message.text.ifBlank { "Waiting for output..." }, fontFamily = if (technical) FontFamily.Monospace else FontFamily.Default, style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
private fun ApprovalDialog(approval: ApprovalUi, model: MainViewModel) {
    AlertDialog(
        onDismissRequest = {},
        icon = { Icon(Icons.Default.Computer, contentDescription = null, tint = Coral) },
        title = { Text(approval.title) },
        text = { Text(approval.detail, fontFamily = FontFamily.Monospace) },
        confirmButton = {
            Button(onClick = { model.approve("accept") }) {
                Icon(Icons.Default.Check, contentDescription = null)
                Spacer(Modifier.width(6.dp))
                Text("Approve once")
            }
        },
        dismissButton = {
            OutlinedButton(onClick = { model.approve("decline") }, colors = ButtonDefaults.outlinedButtonColors(contentColor = Coral)) {
                Icon(Icons.Default.Close, contentDescription = null)
                Spacer(Modifier.width(6.dp))
                Text("Decline")
            }
        },
    )
}

@Composable
private fun ConnectionLabel(status: RemoteClient.ConnectionStatus) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        StatusDot(status == RemoteClient.ConnectionStatus.CONNECTED)
        Spacer(Modifier.width(6.dp))
        Text(status.name.lowercase().replaceFirstChar { it.uppercase() }, style = MaterialTheme.typography.labelSmall, color = Muted)
    }
}

@Composable
private fun StatusDot(active: Boolean) {
    Box(Modifier.size(8.dp).background(if (active) Moss else Coral, RoundedCornerShape(4.dp)))
}

@Composable
private fun EmptyState() {
    Column(Modifier.fillMaxWidth().padding(40.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Text("No projects yet", fontWeight = FontWeight.Medium)
        Text("Add a project on Windows, then refresh.", color = Muted, style = MaterialTheme.typography.bodySmall)
    }
}

@Composable
private fun ErrorBanner(message: String) {
    Box(Modifier.fillMaxWidth().statusBarsPadding().padding(12.dp).background(Color(0xFFFFE9E5), RoundedCornerShape(6.dp)).padding(12.dp)) {
        Text(message, color = Coral, style = MaterialTheme.typography.bodySmall)
    }
}
