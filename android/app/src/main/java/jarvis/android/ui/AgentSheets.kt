package jarvis.android.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Notifications
import androidx.compose.material.icons.filled.NotificationsOff
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import jarvis.android.JarvisApp
import jarvis.android.data.ApiClient
import jarvis.android.data.Hub
import jarvis.android.data.Launcher
import jarvis.android.data.Machine
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import kotlinx.coroutines.launch

/** Long press on an agent: pin, mute, rename, close. Changes show at once and are put back if the hub refuses. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AgentActionsSheet(
    hub: Hub, s: Session, pinned: Boolean, muted: Boolean, machine: String?,
    onDismiss: () -> Unit, onError: (String) -> Unit,
) {
    var renaming by remember { mutableStateOf(false) }
    var closing by remember { mutableStateOf(false) }

    // The app scope, not the sheet's: dismissing the sheet would cancel its own scope halfway through the call.
    fun act(what: String, block: suspend () -> Unit) {
        onDismiss()
        JarvisApp.instance.scope.launch { runCatching { block() }.onFailure { onError("$what 没成功：${it.message}") } }
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.navigationBarsPadding().padding(bottom = 8.dp)) {
            Text(
                s.displayTitle, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(horizontal = 24.dp),
            )
            Text(
                listOfNotNull(machine, s.spaceName?.takeIf { it.isNotBlank() }, s.slotName?.takeIf { it.isNotBlank() }).joinToString(" · "),
                style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 24.dp, end = 24.dp, bottom = 8.dp),
            )
            ListItem(
                headlineContent = { Text(if (pinned) "取消置顶" else "置顶") },
                leadingContent = { Icon(if (pinned) Icons.Outlined.PushPin else Icons.Default.PushPin, null) },
                modifier = Modifier.clickable { act(if (pinned) "取消置顶" else "置顶") { hub.togglePin(s) } },
            )
            ListItem(
                headlineContent = { Text(if (muted) "开启通知" else "屏蔽通知") },
                supportingContent = { if (!muted) Text("不再弹提问和完成的通知，列表里弱化显示") },
                leadingContent = { Icon(if (muted) Icons.Default.Notifications else Icons.Default.NotificationsOff, null) },
                modifier = Modifier.clickable { act(if (muted) "开启通知" else "屏蔽通知") { hub.toggleMute(s) } },
            )
            if (s.isLive) {
                ListItem(
                    headlineContent = { Text("重命名…") },
                    supportingContent = { Text("同时改 herdr 的标签页名和 Claude 的会话名") },
                    leadingContent = { Icon(Icons.Default.Edit, null) },
                    modifier = Modifier.clickable { renaming = true },
                )
                HorizontalDivider(Modifier.padding(vertical = 4.dp))
                ListItem(
                    headlineContent = { Text("关闭 Agent…", color = MaterialTheme.colorScheme.error) },
                    leadingContent = { Icon(Icons.Default.Close, null, tint = MaterialTheme.colorScheme.error) },
                    modifier = Modifier.clickable { closing = true },
                )
            }
        }
    }

    if (renaming) {
        RenameDialog(
            title = "重命名", current = s.displayTitle, note = null,
            onDismiss = { renaming = false },
            onSave = { name -> renaming = false; act("重命名") { hub.api.rename(s.key, name) } },
        )
    }

    if (closing) {
        AlertDialog(
            onDismissRequest = { closing = false },
            title = { Text("关闭这个 Agent？") },
            text = {
                Text(
                    "会退出 ${machine ?: "电脑"} 上的 ${s.agentLabel}，并关掉它在 herdr 里的标签页" +
                        (s.slotName?.takeIf { it.isNotBlank() }?.let { "「$it」" } ?: "") +
                        "（分屏的标签页只关它这一格）。正在做的事会中断。\n对话记录还在，之后可以在「最近 7 天」里发消息恢复。",
                )
            },
            confirmButton = {
                TextButton(onClick = { closing = false; act("关闭") { hub.api.close(s.key) } }) {
                    Text("关闭 Agent", color = MaterialTheme.colorScheme.error, maxLines = 1)
                }
            },
            dismissButton = { TextButton(onClick = { closing = false }) { Text("取消", maxLines = 1) } },
        )
    }
}

/**
 * Starts claude on one of the computers: pick the machine, a directory (recent ones first, or browse from home),
 * the launcher, and an optional first message. The hub opens a new herdr tab there (docs/protocol.md「REST」).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NewSessionSheet(
    hub: Hub, machines: List<Machine>, onDismiss: () -> Unit, onStarted: (SessionKey) -> Unit,
    /** From a space page: start a new tab in this workspace, in its folder (machine and folder can't change). */
    preset: SpacePreset? = null,
) {
    val scope = rememberCoroutineScope()
    var machine by remember { mutableStateOf(machines.firstOrNull { it.id == preset?.machineId } ?: machines.firstOrNull()) }
    var dir by remember { mutableStateOf<ApiClient.DirList?>(null) }
    var picked by remember { mutableStateOf<String?>(null) }
    var launchers by remember { mutableStateOf<List<Launcher>>(emptyList()) }
    var launcher by remember { mutableStateOf<String?>(null) }
    var prompt by remember { mutableStateOf("") }
    var mode by remember { mutableStateOf("") } // "" = whatever the launcher uses
    var loading by remember { mutableStateOf(false) }
    var starting by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }

    fun browse(path: String) {
        val m = machine ?: return
        loading = true; error = null
        scope.launch {
            runCatching { hub.api.dirs(m.id, path) }.onSuccess { dir = it }.onFailure { error = it.message }
            loading = false
        }
    }

    LaunchedEffect(machine?.id) {
        val m = machine ?: return@LaunchedEffect
        dir = null; picked = preset?.cwd; launcher = null
        if (preset == null) browse("")
        runCatching { hub.api.launchers(m.id) }.onSuccess { l -> launchers = l; launcher = l.firstOrNull()?.name }
    }

    ModalBottomSheet(onDismissRequest = { if (!starting) onDismiss() }, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(Modifier.navigationBarsPadding().padding(horizontal = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(if (preset != null) "在「${preset.spaceName}」开新标签" else "新开会话", style = MaterialTheme.typography.titleLarge)
            if (preset != null) Text(tailOf(preset.cwd), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (machines.size > 1 && preset == null) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    machines.forEach { m ->
                        FilterChip(machine?.id == m.id, { machine = m }, label = { Text(m.displayName, maxLines = 1) }, enabled = !starting)
                    }
                }
            }

            val d = dir
            if (preset == null) Row(verticalAlignment = Alignment.CenterVertically) {
                if (d?.parent != null) IconButton(onClick = { browse(d.parent) }, enabled = !starting) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "上一级") }
                Text(
                    tailOf(picked ?: d?.path ?: ""), style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (loading) CircularProgressIndicator(Modifier.padding(8.dp))
            }
            if (preset == null) LazyColumn(Modifier.heightIn(max = 320.dp)) {
                val recent = d?.recent.orEmpty()
                // The hub sends recent directories only with the home listing (an empty path).
                if (recent.isNotEmpty()) {
                    item { Text("最近用过", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary) }
                    items(recent, key = { "r/" + it.path }) { r ->
                        DirRow(r.name.ifBlank { r.path.substringAfterLast('/') }, r.path, recent = true, selected = picked == r.path) { picked = r.path }
                    }
                    item { Text("浏览", style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary, modifier = Modifier.padding(top = 8.dp)) }
                }
                items(d?.entries.orEmpty(), key = { "e/" + it.path }) { e ->
                    DirRow(e.name, if (e.isGit) "git 仓库" else null, recent = false, selected = picked == e.path) {
                        // A tap picks the folder; a second tap on the picked one goes inside it.
                        if (picked == e.path) { picked = null; browse(e.path) } else picked = e.path
                    }
                }
            }

            if (launchers.size > 1) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    launchers.forEach { l -> FilterChip(launcher == l.name, { launcher = l.name }, label = { Text(l.name, maxLines = 1) }, enabled = !starting) }
                }
            }
            // The same choices as iOS (SessionModes.permissionModes); bypass can only be picked when starting.
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                PERMISSION_MODES.forEach { (id, title) ->
                    FilterChip(mode == id, { mode = id }, label = { Text(title, maxLines = 1) }, enabled = !starting)
                }
            }
            OutlinedTextField(
                prompt, { prompt = it }, placeholder = { Text("第一条消息（可以不填）") },
                modifier = Modifier.fillMaxWidth().heightIn(max = 140.dp), enabled = !starting,
            )
            error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
            val cwd = picked ?: d?.path
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(bottom = 16.dp)) {
                OutlinedButton(onClick = onDismiss, enabled = !starting) { Text("取消", maxLines = 1) }
                Button(
                    onClick = {
                        val m = machine ?: return@Button
                        starting = true; error = null
                        val panesBefore = hub.livePanes(m.id)
                        scope.launch {
                            runCatching { hub.api.start(m.id, cwd!!, launcher ?: "claude", prompt.trim().ifEmpty { null }, mode.ifEmpty { null }, preset?.workspaceId) }
                                .onSuccess { onStarted(it.key.copy(machineId = m.id)) }
                                .onFailure { e ->
                                    // Often claude did start and the node only gave up on it:
                                    //  - it stopped on a dialog (trust this folder, the bypass warning…), which the hub turns
                                    //    into a card the user can answer in the session;
                                    //  - its footer is one the node doesn't recognise yet ("did not become ready").
                                    // Either way, if a session came up in that directory since we pressed start, open it.
                                    val msg = e.message.orEmpty()
                                    val maybeUp = "agent_blocked" in msg || "did not become ready" in msg
                                    val fresh = if (maybeUp) { hub.refresh(); hub.startedIn(m.id, cwd!!, panesBefore) } else null
                                    if (fresh != null) onStarted(fresh.key)
                                    else { error = "没启动起来：${msg.lineSequence().firstOrNull()}"; starting = false }
                                }
                        }
                    },
                    enabled = !starting && machine != null && !cwd.isNullOrBlank(),
                    modifier = Modifier.weight(1f),
                ) {
                    if (starting) CircularProgressIndicator(Modifier.padding(end = 8.dp), strokeWidth = 2.dp)
                    Text(if (starting) "正在启动…" else "在这里启动", maxLines = 1)
                }
            }
        }
    }
}

@Composable
private fun DirRow(name: String, detail: String?, recent: Boolean, selected: Boolean, onClick: () -> Unit) {
    ListItem(
        headlineContent = { Text(name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        supportingContent = detail?.let { { Text(tailOf(it), maxLines = 1, overflow = TextOverflow.Ellipsis) } },
        leadingContent = { Icon(if (recent) Icons.Default.History else Icons.Default.Folder, null) },
        tonalElevation = if (selected) 6.dp else 0.dp,
        modifier = Modifier.clickable(onClick = onClick),
    )
}

/** The end of a long path ("…/code/foo/bar"): the part that tells folders apart (no start-ellipsis in this Compose). */
fun tailOf(path: String, keep: Int = 3): String {
    val parts = path.trimEnd('/').split('/')
    return if (parts.size <= keep + 1) path else "…/" + parts.takeLast(keep).joinToString("/")
}

/** Permission modes to start with (iOS SessionModes.permissionModes); "" leaves it to the launcher. */
val PERMISSION_MODES = listOf(
    "" to "默认",
    "default" to "手动确认",
    "acceptEdits" to "自动改文件",
    "plan" to "只做计划",
    "auto" to "自动",
    "bypassPermissions" to "全部放行",
)

/** Rename with the current name selected and focused: typing replaces it, a tap puts the cursor in it. */
@Composable
fun RenameDialog(title: String, current: String, note: String?, onDismiss: () -> Unit, onSave: (String) -> Unit) {
    var name by remember { mutableStateOf(TextFieldValue(current, TextRange(0, current.length))) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }
    val trimmed = name.text.trim()
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(name, { name = it }, singleLine = true, modifier = Modifier.fillMaxWidth().focusRequester(focus))
                note?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
        },
        confirmButton = { TextButton(onClick = { onSave(trimmed) }, enabled = trimmed.isNotEmpty() && trimmed != current) { Text("保存", maxLines = 1) } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("取消", maxLines = 1) } },
    )
}

/** Where the space page's "在这个空间开新标签" starts claude. */
data class SpacePreset(val machineId: String, val cwd: String, val workspaceId: String, val spaceName: String)
