package jarvis.feature.agents

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import jarvis.core.contracts.Machine
import jarvis.core.designsystem.*
import jarvis.core.designsystem.icons.PhIcons
import java.net.URI

@Composable
internal fun NewAgentDialog(model: NewAgentViewModel, machines: List<Machine>, online: Boolean, onDismiss: () -> Unit) {
    val state by model.state.collectAsStateWithLifecycle()
    var query by remember { mutableStateOf("") }
    val editable = !state.locked && !state.loading
    val close = { if (!state.starting) { if (state.browsing) model.closeBrowser() else onDismiss() } }
    JSheet(onDismiss = { if (!state.starting) onDismiss() }, title = "新建会话") {
        BackHandler(onBack = close)
        Column(Modifier.fillMaxWidth().weight(1f).imePadding()) {
            LazyColumn(Modifier.weight(1f), contentPadding = PaddingValues(bottom = 16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (state.loading) item("loading") { JLinearProgress(null) }
                if (state.browsing) {
                    item("browser-title") {
                        JSectionHeader("选择目录", action = { JTextButton("返回", close, enabled = !state.starting) })
                        Text(state.browser?.path ?: state.browsePath, style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                    }
                    state.browser?.let { directory ->
                        if (directory.parent.isNotBlank() && directory.parent != directory.path) item("parent") {
                            JTextButton("上一级", { model.browse(directory.parent) }, enabled = !state.loading)
                        }
                        if (directory.entries.isEmpty()) item("empty-directory") {
                            Text("这个目录下没有子目录", style = JarvisTheme.type.callout, color = JarvisTheme.palette.ink2)
                        }
                        itemsIndexed(directory.entries, key = { _, entry -> entry.path }) { index, entry ->
                            JGroupItem(GroupPosition.of(index, directory.entries.size)) {
                                JRow(entry.name, subtitle = if (entry.isGit) "Git 仓库" else null, leadingIcon = PhIcons.FolderSimple,
                                    enabled = !state.loading, onClick = { model.browse(entry.path) })
                            }
                        }
                    }
                    if (!state.loading && state.browser == null) item("retry-browser") {
                        JButton("重试打开", { model.browse(state.browsePath) }, kind = JButtonKind.Secondary)
                    }
                } else {
                    val space = state.space
                    // Existing spaces keep their machine and directory fixed.
                    if (space != null) item("space") {
                        val machine = machines.firstOrNull { it.id == space.machineId }
                        JSectionHeader("在「${space.name}」开新标签")
                        JRow(machine?.displayName ?: space.machineId, value = if (machine?.online == true) "在线" else "离线", leadingIcon = PhIcons.Laptop)
                        JRow("目录", subtitle = space.cwd, leadingIcon = PhIcons.FolderSimple)
                        if (machine?.online != true) Text("这台机器离线，连上后才能开新标签", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                    } else item("machines") {
                        JSectionHeader("机器")
                        Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            machines.forEach { machine ->
                                JChip(machine.displayName, selected = machine.id == state.machineId, icon = PhIcons.Laptop,
                                    status = if (machine.online) Status.Idle else Status.Offline,
                                    onClick = if (editable && machine.online) ({ query = ""; model.selectMachine(machine.id) }) else null)
                            }
                        }
                        if (machines.none { it.online }) Text("暂无在线机器，请先让机器连上 hub", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                    }
                    if (state.error != null && !state.loading && state.pending == null && state.machineId.isNotBlank()) item("reload") {
                        JButton("重新加载选项", { model.selectMachine(state.machineId) }, enabled = !state.locked, kind = JButtonKind.Secondary)
                    }
                    if (space == null) {
                        item("directory") {
                            JSectionHeader("目录")
                            JGroupItem(GroupPosition.Single) {
                                JRow("选择目录", subtitle = state.selection.cwd.takeIf { it.isNotBlank() }, leadingIcon = PhIcons.FolderSimple,
                                    enabled = editable && state.machineId.isNotBlank(), onClick = { model.browse() })
                            }
                        }
                        itemsIndexed(state.recent, key = { _, recent -> "recent:${recent.path}" }) { index, recent ->
                            JGroupItem(GroupPosition.of(index, state.recent.size)) {
                                JRow(recent.name.ifEmpty { recent.path.substringAfterLast('/') }, subtitle = recent.path,
                                    leading = { JRadio(state.selection.cwd == recent.path, null, enabled = editable) }, enabled = editable,
                                    onClick = { model.selection(state.selection.copy(cwd = recent.path)) }, chevron = false)
                            }
                        }
                    }
                    item("agent-types-title") { JSectionHeader("Agent 类型") }
                    val types = state.launchers.map { it.agentType }.distinct()
                    val selectedType = state.launchers.firstOrNull { it.name == state.selection.launcher }?.agentType
                    itemsIndexed(types, key = { _, type -> "type:$type" }) { index, type ->
                        JGroupItem(GroupPosition.of(index, types.size)) {
                            JRow(type.agentLabel(), leading = { JRadio(selectedType == type, null, enabled = editable) }, enabled = editable,
                                onClick = { model.selection(state.selection.copy(launcher = state.launchers.first { it.agentType == type }.name)) }, chevron = false)
                        }
                    }
                    item("launcher-search") {
                        JSectionHeader("启动器")
                        JSearchField(query, { if (editable) query = it }, "搜索启动器或模型")
                        if (!state.loading && state.launchers.isEmpty()) Text("这台机器没有可用启动器", style = JarvisTheme.type.caption, color = JarvisTheme.palette.ink2)
                    }
                    val launchers = state.launchers.filter { launcher ->
                        launcher.agentType == (selectedType ?: "claude") &&
                            (query.isBlank() || launcher.name.contains(query, true) || launcher.model.orEmpty().contains(query, true))
                    }
                    itemsIndexed(launchers, key = { _, launcher -> "launcher:${launcher.name}" }) { index, launcher ->
                        val host = launcher.baseUrl?.let { runCatching { URI(it).host }.getOrNull() }
                        JGroupItem(GroupPosition.of(index, launchers.size)) {
                            JRow(launcher.name, subtitle = listOfNotNull(launcher.agentType.agentLabel(), launcher.model, host).joinToString("\n"),
                                subtitleLines = 3, leading = { JRadio(state.selection.launcher == launcher.name, null, enabled = editable) },
                                enabled = editable, onClick = { model.selection(state.selection.copy(launcher = launcher.name)) }, chevron = false)
                        }
                    }
                    if ((selectedType ?: "claude") == "claude") item("claude-options") {
                        ChoiceMenu("权限模式", state.selection.mode, permissionModes, editable) { model.selection(state.selection.copy(mode = it)) }
                        if (state.selection.mode == "bypassPermissions") Text("全部放行后，改文件和执行命令都不会再询问你。", color = JarvisTheme.palette.danger.text, style = JarvisTheme.type.caption)
                        ChoiceMenu("思考强度", state.selection.effort, effortLevels, editable) { model.selection(state.selection.copy(effort = it)) }
                    }
                    item("prompt") {
                        JTextField(state.prompt, model::prompt, enabled = !state.locked, label = "第一条消息（可空）",
                            minLines = 3, maxLines = 8, modifier = Modifier.fillMaxWidth())
                    }
                }
            }
            Column(Modifier.fillMaxWidth().padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (state.pending != null) PendingBadge(if (state.starting) PendingStatus.Verifying else PendingStatus.Unconfirmed)
                state.error?.let { Text(it, color = JarvisTheme.palette.ink2, style = JarvisTheme.type.callout) }
                if (!online) Text("连上 hub 后才能启动或核对", color = JarvisTheme.palette.ink2, style = JarvisTheme.type.callout)
                if (state.browsing) {
                    JButton("使用此目录", { model.closeBrowser(use = true) }, enabled = state.browser != null && !state.loading, modifier = Modifier.fillMaxWidth())
                } else {
                    JButton(if (state.pending != null) "重试核对结果" else "开始对话", model::submit, loading = state.starting,
                        enabled = online && !state.starting && !state.loading && state.initialized &&
                            (state.pending != null || (state.selection.cwd.isNotBlank() && state.selection.launcher.isNotBlank() &&
                                machines.any { it.id == state.machineId && it.online })), modifier = Modifier.fillMaxWidth())
                }
            }
        }
    }
}

private fun String.agentLabel() = when (this) { "claude" -> "Claude Code"; "codex" -> "Codex"; "traex" -> "TRAEX"; else -> this }
private val permissionModes = listOf("" to "跟随启动器", "default" to "手动确认", "acceptEdits" to "自动改文件", "plan" to "只做计划", "auto" to "自动", "bypassPermissions" to "全部放行")
private val effortLevels = listOf("" to "跟随启动器", "low" to "低", "medium" to "中", "high" to "高", "xhigh" to "很高", "max" to "最高")

@Composable
private fun ChoiceMenu(title: String, value: String, options: List<Pair<String, String>>, enabled: Boolean, onSelect: (String) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    JRow(title, value = options.firstOrNull { it.first == value }?.second ?: value, enabled = enabled, onClick = { expanded = true })
    if (expanded) JActionSheet(options.map { (id, label) -> JAction(label, checked = id == value, onClick = { onSelect(id) }) },
        { expanded = false }, title = title)
}
