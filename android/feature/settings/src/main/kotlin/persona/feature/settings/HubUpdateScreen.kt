package persona.feature.settings

import android.content.ClipData
import android.content.ClipboardManager
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.style.TextOverflow
import androidx.navigation3.runtime.NavKey
import persona.core.contracts.Machine
import persona.core.contracts.api.HubUpdateRecord
import persona.core.contracts.api.HubUpdateStatus
import persona.core.contracts.api.HubUpdates
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.MachineDetailRoute
import kotlinx.coroutines.launch

/**
 * 设置 › hub 版本 (docs/hub-update.md「App 里」, iOS `HubSettingsPage`, the web's `HubSettings`): the hub's version and
 * channel, checking for and installing a release, what an update waits for (with 现在就更新), a Docker hub's commands,
 * auto-update, the personad on each computer, and the history. Follows the hub's `hub_update` pushes.
 */
@Composable
internal fun HubUpdatePage(
    updates: HubUpdates?, machines: List<Machine>, onBack: () -> Unit,
    /** The hub's connection is up: read again each time it comes back (after a restart the pushes may have been missed). */
    online: Boolean = true,
    /** Opens a computer's page (重新部署 lives there); null where there is no navigation host. */
    onMachine: ((NavKey) -> Unit)? = null,
) {
    if (updates == null) {
        JListPage(title = "hub 版本", onBack = onBack) { item { JNotice("体验模式不会连接 hub，也不会更新它。", Modifier.padding(top = Tokens.Space.m)) } }
        return
    }
    val model = remember(updates) { HubUpdateModel(updates) }
    val state by model.state.collectAsState()
    HubUpdateContent(state, machines, onBack, onMachine, model)
    LaunchedEffect(model, online) { if (online) model.load() }
    LaunchedEffect(model) { updates.events.collect { model.receive(it) } }
}

@Composable
internal fun HubUpdateContent(
    state: HubUpdateState, machines: List<Machine>, onBack: () -> Unit, onMachine: ((NavKey) -> Unit)?, model: HubUpdateModel?,
) {
    val scope = rememberCoroutineScope()
    var confirmNow by remember { mutableStateOf(false) }
    val s = state.status
    JListPage(title = "hub 版本", onBack = onBack, refreshing = state.checking && s != null,
        onRefresh = model?.let { m -> { scope.launch { m.check() } } }) {
        when {
            state.unsupported -> item("unsupported") { JNotice(HubUpdateText.Unsupported, Modifier.padding(top = Tokens.Space.m)) }
            s != null -> {
                state.actionError?.let { item("action-error") { JInlineStatus(it, Modifier.padding(top = Tokens.Space.m), tone = InlineTone.Failed) } }
                // Told once that it updates itself (the hub forgets the line once seen or once auto-update was chosen).
                if (s.autoNotice) item("auto-notice") {
                    // Like the web's line: 关闭 turns auto-update off, 知道了 only hides this (both tell the hub it was seen).
                    JCard(Modifier.padding(top = Tokens.Space.m)) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                            JIcon(PhIcons.Info, null, tint = PersonaTheme.palette.accent, decorative = true)
                            Text(HubUpdateText.AutoNotice, Modifier.weight(1f), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink)
                            JButton("关闭", { model?.let { m -> scope.launch { m.setAuto(false) } } }, kind = JButtonKind.Quiet, size = JButtonSize.Small, enabled = !state.sending)
                            JButton("知道了", { model?.let { m -> scope.launch { m.noticeSeen() } } }, kind = JButtonKind.Quiet, size = JButtonSize.Small, enabled = !state.sending)
                        }
                    }
                }
                version(s, state, onCheck = { model?.let { m -> scope.launch { m.check() } } })
                update(s, state, onUpdate = { model?.let { m -> scope.launch { m.update(now = false) } } }, onNow = { confirmNow = true })
                if (s.canUpdate) item("auto") {
                    Column {
                        JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.xl)) {
                            JSwitchRow("自动更新 hub", s.auto, { on -> model?.let { m -> scope.launch { m.setAuto(on) } } }, enabled = !state.sending)
                        }
                        Note(HubUpdateText.AutoFooter)
                    }
                }
                nodes(machines, onMachine)
                if (state.history.isNotEmpty()) history(state.history)
            }
            state.error != null -> item("error") {
                JNotice(state.error, Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou,
                    actionLabel = "重试", onAction = model?.let { m -> { scope.launch { m.load() } } })
            }
            else -> item("loading") { JLoading() }
        }
    }
    if (confirmNow) JDialog(
        onDismiss = { confirmNow = false }, title = "现在就更新？", text = HubUpdateText.NowWarning, destructive = true,
        confirm = "现在就更新" to { confirmNow = false; model?.let { m -> scope.launch { m.update(now = true) } }; Unit },
    )
}

private fun LazyListScope.version(s: HubUpdateStatus, state: HubUpdateState, onCheck: () -> Unit) {
    val rows = buildList<@Composable () -> Unit> {
        add { JRow("版本", value = s.version.ifBlank { "未知" }) }
        add { JRow("渠道", value = HubUpdateText.channel(s.channel)) }
        HubUpdateText.lastCheck(s)?.let { at -> add { JRow("上次检查", value = at) } }
        add {
            JRow(onClick = onCheck, enabled = !state.checking && !s.working, chevron = false,
                trailing = if (state.checking) ({ JSpinner() }) else null) {
                Text(if (state.checking) "正在检查…" else "检查更新", style = PersonaTheme.type.body, color = PersonaTheme.palette.accent,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        }
    }
    item("version-header") { JSectionHeader("版本") }
    itemsIndexed(rows, key = { index, _ -> "version-$index" }) { index, row -> JGroupItem(GroupPosition.of(index, rows.size)) { row() } }
}

private fun LazyListScope.update(s: HubUpdateStatus, state: HubUpdateState, onUpdate: () -> Unit, onNow: () -> Unit) {
    item("update-header") { JSectionHeader("更新") }
    item("update") {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            when (val view = HubUpdateText.view(s)) {
                HubUpdateView.Docker -> DockerRows(s)
                is HubUpdateView.Cannot -> JGroupItem(GroupPosition.Single) { JRow { Callout(view.why) } }
                is HubUpdateView.Newer -> {
                    val latest = s.latest!!
                    JGroupItem(GroupPosition.Single) {
                        JRow {
                            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                                    Text("新版本 ${latest.version}", Modifier.weight(1f, fill = false), style = PersonaTheme.type.bodyStrong,
                                        color = PersonaTheme.palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                                    HubUpdateText.released(s)?.let { Text(it, style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3, maxLines = 1) }
                                }
                                latest.notes?.takeIf { it.isNotBlank() }?.let { Callout(it) }
                            }
                        }
                    }
                    HubUpdateText.latestWarning(s)?.let { JInlineStatus(it, tone = InlineTone.Caution) }
                    if (view.working) Progress(s, onNow)
                    else JButton("更新 hub", onUpdate, Modifier.fillMaxWidth(), loading = state.sending, loadingText = "正在开始", enabled = !state.sending)
                }
                HubUpdateView.Working -> Progress(s, onNow)
                HubUpdateView.Current -> JGroupItem(GroupPosition.Single) {
                    JRow {
                        JIcon(PhIcons.CheckCircle, null, tint = PersonaTheme.palette.positive, decorative = true)
                        Spacer(Modifier.width(Tokens.Space.m))
                        Text("已是最新", style = PersonaTheme.type.body, color = PersonaTheme.palette.positive, maxLines = 1)
                    }
                }
            }
            s.error?.takeIf { it.isNotBlank() && !s.working }?.let {
                JInlineStatus(it, tone = if (s.state == "rolled_back") InlineTone.Caution else InlineTone.Failed)
            }
        }
    }
    if (s.canUpdate) item("update-footer") { Note(HubUpdateText.UpdateFooter) }
}

/** The update under way, and while it waits for idle what it waits for, with 现在就更新. */
@Composable
private fun Progress(s: HubUpdateStatus, onNow: () -> Unit) {
    val waiting = s.state == "waiting_idle" && s.busy.isNotEmpty()
    val rows = buildList<@Composable () -> Unit> {
        add {
            JRow {
                JSpinner()
                Spacer(Modifier.width(Tokens.Space.m))
                Text(HubUpdateText.state(s), Modifier.weight(1f, fill = false), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink,
                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                s.progress?.takeIf { s.state == "downloading" && it.isNotBlank() }?.let {
                    Spacer(Modifier.width(Tokens.Space.s))
                    Text(it, style = PersonaTheme.type.codeSmall, color = PersonaTheme.palette.ink3, maxLines = 1)
                }
            }
        }
        if (waiting) s.busy.forEach { b -> add { JRow(b.title.ifBlank { "进行中" }, subtitle = b.machine.ifBlank { null }, leadingIcon = PhIcons.HourglassMedium) } }
    }
    Column {
        rows.forEachIndexed { index, row -> JGroupItem(GroupPosition.of(index, rows.size)) { row() } }
    }
    if (waiting) JButton("现在就更新…", onNow, Modifier.fillMaxWidth(), kind = JButtonKind.Destructive)
}

/** A Docker hub updates by a new image: what to run where, each command with a copy button. */
@Composable
private fun DockerRows(s: HubUpdateStatus) {
    val (headline, steps) = HubUpdateText.dockerSteps(s)
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    headline?.let { Text(it, Modifier.padding(horizontal = rowPadding), style = PersonaTheme.type.bodyStrong, color = PersonaTheme.palette.ink, maxLines = 1) }
    steps.forEach { step ->
        Text(step.text, Modifier.padding(horizontal = rowPadding), style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
        JGroupItem(GroupPosition.Single) {
            JRow(trailing = {
                JIconButton(PhIcons.Copy, "复制", {
                    context.getSystemService(ClipboardManager::class.java)?.setPrimaryClip(ClipData.newPlainText("命令", step.copy))
                    feedback?.notice("已复制")
                })
            }) {
                Text(step.copy, style = PersonaTheme.type.codeSmall, color = PersonaTheme.palette.ink, maxLines = 2, overflow = TextOverflow.Ellipsis)
            }
        }
    }
}

private fun LazyListScope.nodes(machines: List<Machine>, onMachine: ((NavKey) -> Unit)?) {
    if (machines.isEmpty()) return
    item("nodes-header") { JSectionHeader("各台电脑上的 personad") }
    itemsIndexed(machines, key = { _, m -> "node-${m.id}" }) { index, m ->
        JGroupItem(GroupPosition.of(index, machines.size)) {
            JRow(m.displayName, value = m.nodeVersion?.takeIf { it.isNotBlank() } ?: if (m.online) "未知" else "离线",
                onClick = onMachine?.let { go -> { go(MachineDetailRoute(m.id)) } },
                trailing = if (m.nodeMismatch == true) ({ JChip("版本不同", status = Status.Working) }) else null,
                chevron = onMachine != null)
        }
    }
    item("nodes-footer") { Note(HubUpdateText.NodesFooter) }
}

private fun LazyListScope.history(records: List<HubUpdateRecord>) {
    val shown = records.take(10)
    item("history-header") { JSectionHeader("更新记录") }
    itemsIndexed(shown, key = { index, r -> "record-$index-${r.toBuild}" }) { index, r ->
        JGroupItem(GroupPosition.of(index, shown.size)) { RecordRow(r) }
    }
}

@Composable
private fun RecordRow(r: HubUpdateRecord) {
    val palette = PersonaTheme.palette
    val tone: Color = when (r.result) { "updated" -> palette.positive; "rolled_back", "held" -> palette.cautionText; else -> palette.danger.text }
    JRow {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Text(HubUpdateText.record(r.result), style = PersonaTheme.type.bodyStrong, color = tone, maxLines = 1)
                if (r.auto) JChip("自动")
                Spacer(Modifier.weight(1f))
                HubUpdateText.recordTime(r)?.let { Text(it, style = PersonaTheme.type.caption, color = palette.ink3, maxLines = 1) }
            }
            Text("${r.fromVersion} → ${r.toVersion}", style = PersonaTheme.type.codeSmall, color = palette.ink2, maxLines = 1, overflow = TextOverflow.MiddleEllipsis)
            r.reason?.takeIf { it.isNotBlank() }?.let { Text(it, style = PersonaTheme.type.caption, color = palette.ink3) }
        }
    }
}

@Composable
private fun Callout(text: String) = Text(text, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)

@Composable
private fun Note(text: String) = SettingsCaption(text, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s))
