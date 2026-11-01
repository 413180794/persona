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
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.UnfoldMore
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

/**
 * The two chips above the composer (iOS SessionModeBar): permission mode and effort level, like the footer of Claude
 * Code's TUI. Picking one changes it on the running session. Claude only: Codex / TRAEX have their own policies.
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
    Column(Modifier.fillMaxWidth().padding(horizontal = 12.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
            ModeMenu(
                text = SessionModes.modeLabel(session.permissionMode) ?: "权限模式", known = session.permissionMode != null,
                warn = session.permissionMode == "bypassPermissions", enabled = !changing,
                options = SessionModes.switchable(session.permissionMode), current = session.permissionMode,
            ) { change(mode = it) }
            ModeMenu(
                text = session.effort?.let { "思考" + SessionModes.effortLabel(it) } ?: "思考强度", known = session.effort != null,
                warn = false, enabled = !changing, options = SessionModes.effortLevels, current = session.effort,
            ) { change(effort = it) }
            // /model for Claude in a herdr pane (iOS canPickModel): Codex / TRAEX pick in a menu the phone can't drive.
            if (onPickModel != null && session.paneId != null && !session.assistant) {
                val id = SessionModes.claudeModelId(session.model)
                ModeMenu(
                    text = SessionModes.claudeModels.first { it.id == id }.title, known = session.model != null,
                    warn = false, enabled = !changing, options = SessionModes.claudeModels, current = id,
                ) { if (it != id) onPickModel(it) }
            }
            if (changing) CircularProgressIndicator(Modifier.size(12.dp), strokeWidth = 1.5.dp)
        }
        error?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error) }
    }
}

@Composable
private fun ModeMenu(
    text: String, known: Boolean, warn: Boolean, enabled: Boolean,
    options: List<SessionModes.Option>, current: String?, onPick: (String) -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    val c = MaterialTheme.colorScheme
    Box {
        Row(
            Modifier.clip(RoundedCornerShape(50)).background(if (warn) c.errorContainer else c.surfaceVariant)
                .clickable(enabled = enabled) { open = true }.padding(horizontal = 10.dp, vertical = 5.dp),
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(2.dp),
        ) {
            Text(text, style = MaterialTheme.typography.labelMedium, maxLines = 1,
                color = when { warn -> c.onErrorContainer; known -> c.onSurface; else -> c.onSurfaceVariant })
            Icon(Icons.Default.UnfoldMore, null, Modifier.size(14.dp), tint = c.onSurfaceVariant)
        }
        DropdownMenu(open, onDismissRequest = { open = false }) {
            options.forEach { o ->
                DropdownMenuItem(
                    text = {
                        Column {
                            Text(o.title)
                            if (o.detail.isNotEmpty()) Text(o.detail, style = MaterialTheme.typography.bodySmall, color = c.onSurfaceVariant)
                        }
                    },
                    trailingIcon = { if (o.id == current) Icon(Icons.Default.Check, "当前") },
                    onClick = { open = false; onPick(o.id) },
                )
            }
        }
    }
}
