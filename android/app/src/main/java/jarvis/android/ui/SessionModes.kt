package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.height
import androidx.compose.material3.HorizontalDivider
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.surface
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import jarvis.android.data.Hub
import jarvis.android.data.Session
import kotlinx.coroutines.launch

/** Claude Code's permission modes and effort levels as the app shows them (iOS SessionModes, server node/modes.go). */
object SessionModes {
    data class Option(val id: String, val title: String, val detail: String)

    val permissionModes = listOf(
        Option("default", "手动确认", "改文件、跑命令前都先问你"),
        Option("acceptEdits", "自动改文件", "改文件不问，跑命令还会问"),
        Option("plan", "只做计划", "只读代码、写计划，批准后才动手"),
        Option("auto", "自动", "自己判断风险，安全的直接做"),
        Option("bypassPermissions", "全部放行", "什么都不问；只能在新开时选"),
    )
    val effortLevels = listOf(
        Option("low", "低", "最快，简单的事够用"),
        Option("medium", "中", "快一些"),
        Option("high", "高", "大多数任务的默认"),
        Option("xhigh", "很高", "更仔细，也更慢、更贵"),
        Option("max", "最高", "最慢最仔细"),
    )

    /** What a running session can switch to: not bypassPermissions (only at start), plus the current one when it is
     *  none of these (bypassPermissions, a newer Claude Code mode) so it still shows as picked. */
    fun switchable(current: String?): List<Option> {
        val list = permissionModes.filter { it.id != "bypassPermissions" }
        if (current == null || list.any { it.id == current }) return list
        return list + (permissionModes.firstOrNull { it.id == current } ?: Option(current, current, ""))
    }

    /** Claude Code's `/model` aliases (iOS PromptItems.claudeModels): picking one sends `/model <alias>`. */
    val claudeModels = listOf(
        Option("default", "默认", "账号默认"),
        Option("opus", "Opus", "最强"),
        Option("sonnet", "Sonnet", "均衡"),
        Option("haiku", "Haiku", "最快"),
    )

    /** The alias a session's model reads as ("claude-opus-…" → opus); unknown or none is default. */
    fun claudeModelId(model: String?): String {
        val m = model.orEmpty().lowercase()
        return listOf("opus", "sonnet", "haiku").firstOrNull { it in m } ?: "default"
    }

    fun modeLabel(id: String?): String? = id?.let { m -> permissionModes.firstOrNull { it.id == m }?.title ?: m }
    fun effortLabel(id: String?): String? = id?.let { e -> effortLevels.firstOrNull { it.id == e }?.title ?: e }
}

/** The symbol of a permission mode (iOS `SessionModes.symbol`). */
fun modeIcon(id: String?) = when (id) {
    "acceptEdits" -> JIcons.edit
    "plan" -> JIcons.plan
    "auto" -> JIcons.ai
    "bypassPermissions" -> JIcons.allowEverything
    else -> JIcons.askFirst
}

/**
 * The chips above the composer (iOS SessionModeBar): permission mode and effort level, like the footer of Claude Code's
 * TUI, and the model when [onPickModel] can send `/model`. Picking one changes it on the running session. Claude only:
 * Codex / TRAEX have their own policies.
 */
@Composable
fun SessionModeBar(hub: Hub, session: Session, onPickModel: ((String) -> Unit)? = null) {
    val scope = rememberCoroutineScope()
    var changing by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    fun change(mode: String? = null, effort: String? = null) {
        if (changing || (mode == null || mode == session.permissionMode) && (effort == null || effort == session.effort)) return
        changing = true; error = null
        scope.launch {
            runCatching { hub.setModes(session.key, mode, effort) }.onFailure { error = "没改成：${it.message}" }
            changing = false
        }
    }
    Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalAlignment = Alignment.CenterVertically) {
            ModeMenu(
                text = SessionModes.modeLabel(session.permissionMode) ?: "权限模式", icon = modeIcon(session.permissionMode),
                known = session.permissionMode != null, warn = session.permissionMode == "bypassPermissions", enabled = !changing,
                options = SessionModes.switchable(session.permissionMode), current = session.permissionMode, itemIcon = ::modeIcon,
            ) { change(mode = it) }
            ModeMenu(
                text = session.effort?.let { "思考 " + SessionModes.effortLabel(it) } ?: "思考强度", icon = JIcons.effort, known = session.effort != null,
                warn = false, enabled = !changing, options = SessionModes.effortLevels, current = session.effort, itemIcon = { JIcons.effort },
            ) { change(effort = it) }
            // /model for Claude in a herdr pane (iOS canPickModel): Codex / TRAEX pick in a menu the phone can't drive.
            if (onPickModel != null && session.paneId != null && !session.assistant) {
                val id = SessionModes.claudeModelId(session.model)
                ModeMenu(
                    text = SessionModes.claudeModels.first { it.id == id }.title, icon = JIcons.model, known = session.model != null,
                    warn = false, enabled = !changing, options = SessionModes.claudeModels, current = id, itemIcon = { JIcons.model },
                ) { if (it != id) onPickModel(it) }
            }
            if (changing) CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium, color = JTheme.colors.secondaryText)
        }
        error?.let { Text(it, style = JTheme.type.caption, color = JTheme.colors.danger) }
    }
}

/**
 * The same two choices as items of the shell composer's gauge menu (iOS `SessionModesMenu`): the modes, a divider,
 * the effort levels; the current ones checked.
 */
@Composable
fun ColumnScope.ModeMenuItems(hub: Hub, session: Session, close: () -> Unit) {
    val scope = rememberCoroutineScope()
    fun change(mode: String? = null, effort: String? = null) {
        close()
        if ((mode == null || mode == session.permissionMode) && (effort == null || effort == session.effort)) return
        scope.launch { runCatching { hub.setModes(session.key, mode, effort) }.onFailure { jarvis.android.ui.chat.Toasts.show("没改成：${it.message}", jarvis.android.ui.chat.ToastStyle.ERROR) } }
    }
    Text("权限模式", style = JTheme.type.sectionHeader, color = JTheme.colors.secondaryText, modifier = Modifier.padding(horizontal = JSpace.l, vertical = JSpace.s))
    SessionModes.switchable(session.permissionMode).forEach { o ->
        ModeItem(o, modeIcon(o.id), o.id == session.permissionMode) { change(mode = o.id) }
    }
    HorizontalDivider()
    Text("思考强度", style = JTheme.type.sectionHeader, color = JTheme.colors.secondaryText, modifier = Modifier.padding(horizontal = JSpace.l, vertical = JSpace.s))
    SessionModes.effortLevels.forEach { o -> ModeItem(o, JIcons.effort, o.id == session.effort) { change(effort = o.id) } }
}

@Composable
private fun ModeItem(o: SessionModes.Option, icon: androidx.compose.ui.graphics.vector.ImageVector, current: Boolean, onClick: () -> Unit) {
    DropdownMenuItem(
        text = {
            Column {
                Text(o.title, style = JTheme.type.body, maxLines = 1)
                if (o.detail.isNotEmpty()) Text(o.detail, style = JTheme.type.caption, color = JTheme.colors.secondaryText, maxLines = 2)
            }
        },
        leadingIcon = { Icon(icon, null, tint = if (o.id == "bypassPermissions") JTheme.colors.danger else JTheme.colors.label) },
        trailingIcon = { if (current) Icon(JIcons.done, "当前", tint = JTheme.colors.accent) },
        onClick = onClick,
    )
}

@Composable
private fun ModeMenu(
    text: String, icon: androidx.compose.ui.graphics.vector.ImageVector, known: Boolean, warn: Boolean, enabled: Boolean,
    options: List<SessionModes.Option>, current: String?, itemIcon: (String) -> androidx.compose.ui.graphics.vector.ImageVector,
    onPick: (String) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    val c = JTheme.colors
    val words = when { warn -> c.danger; known -> c.label; else -> c.secondaryText }
    Box {
        Row(
            Modifier.height(JSize.wellS).surface(CircleShape, floating = true, fill = if (warn) c.danger.copy(alpha = 0.12f) else null)
                .clickable(enabled = enabled) { open = true }.padding(horizontal = JSpace.m),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
        ) {
            Icon(icon, null, Modifier.size(JSize.markS), tint = words)
            Text(text, style = JTheme.type.labelMedium, maxLines = 1, color = words)
            Icon(JIcons.expandAll, null, Modifier.size(JSize.markXS), tint = c.tertiaryText)
        }
        DropdownMenu(open, onDismissRequest = { open = false }) {
            options.forEach { o -> ModeItem(o, itemIcon(o.id), o.id == current) { open = false; onPick(o.id) } }
        }
    }
}
