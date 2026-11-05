package persona.feature.chat

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import persona.core.contracts.Session
import persona.core.contracts.api.ChatAction
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.sort.canChangeEffortOnly
import persona.core.domain.sort.canChangeModes
import persona.core.domain.sort.canTypeClaudeCommands

/** Claude Code's permission modes, effort levels and /model aliases as the app shows them (iOS SessionModes, node/modes.go). */
internal object SessionModes {
    data class Option(val id: String, val title: String, val detail: String)

    const val BYPASS = "bypassPermissions"

    val permissionModes = listOf(
        Option("default", "手动确认", "改文件、跑命令前都先问你"),
        Option("acceptEdits", "自动改文件", "改文件不问，跑命令还会问"),
        Option("plan", "只做计划", "只读代码、写计划，批准后才动手"),
        Option("auto", "自动", "自己判断风险，安全的直接做"),
        Option(BYPASS, "全部放行", "什么都不问；只能在新开时选"),
    )

    val effortLevels = listOf(
        Option("low", "低", "最快，简单的事够用"),
        Option("medium", "中", "快一些"),
        Option("high", "高", "大多数任务的默认"),
        Option("xhigh", "很高", "更仔细，也更慢、更贵"),
        Option("max", "最高", "最慢最仔细"),
    )

    /** Codex's reasoning efforts (node/codex_effort.go, iOS SessionModes.codexEffortLevels); a thread offers its model's. */
    val codexEffortLevels = listOf(
        Option("minimal", "极低", "几乎不思考，最快"),
        Option("low", "低", "快，简单的事够用"),
        Option("medium", "中", "快慢适中"),
        Option("high", "高", "更仔细"),
        Option("xhigh", "很高", "更仔细，也更慢、更贵"),
        Option("max", "最高", "最慢最仔细"),
        Option("ultra", "极高", "最仔细，还会主动分给多个子 Agent 一起做"),
    )

    /** A running session's effort choices: its own levels when the hub lists them (a Codex thread on its app-server), Claude Code's otherwise. */
    fun effortOptions(session: Session): List<Option> =
        if (session.efforts.isEmpty()) effortLevels
        else session.efforts.map { id -> codexEffortLevels.firstOrNull { it.id == id } ?: Option(id, id, "") }

    /** Claude Code's `/model` aliases (iOS PromptItems.claudeModels): picking one sends `/model <alias>`. */
    val claudeModels = listOf(
        Option("default", "默认", "账号默认"),
        Option("opus", "Opus", "最强"),
        Option("sonnet", "Sonnet", "均衡"),
        Option("haiku", "Haiku", "最快"),
    )

    /**
     * What a running session can switch to: not bypassPermissions (only at start), plus the current mode when it is
     * none of these (bypassPermissions, or a newer Claude Code's mode, shown by its raw name), so it still shows as picked.
     */
    fun switchable(current: String?): List<Option> {
        val list = permissionModes.filter { it.id != BYPASS }
        if (current.isNullOrEmpty() || list.any { it.id == current }) return list
        return list + (permissionModes.firstOrNull { it.id == current } ?: Option(current, current, "这个版本的 App 还不认识这个模式"))
    }

    /** The alias a session's model reads as ("claude-opus-…" → opus); a gateway model or none is the default. */
    fun claudeModelId(model: String?): String {
        val name = model.orEmpty().lowercase()
        return listOf("opus", "sonnet", "haiku").firstOrNull { it in name } ?: "default"
    }

    fun modeLabel(id: String?): String? = id?.let { mode -> permissionModes.firstOrNull { it.id == mode }?.title ?: mode }
    fun effortLabel(id: String?): String? = id?.let { effort ->
        (effortLevels.firstOrNull { it.id == effort } ?: codexEffortLevels.firstOrNull { it.id == effort })?.title ?: effort
    }
}

/** Non-default settings in one line above the composer ("自动改文件，思考很高"). */
internal fun chatModeHint(session: Session?): String? = listOfNotNull(
    session?.permissionMode?.takeIf { it != "default" }?.let { SessionModes.modeLabel(it) },
    session?.effort?.takeIf { it != "high" }?.let { "思考${SessionModes.effortLabel(it)}" },
).takeIf { it.isNotEmpty() }?.joinToString("，")

/**
 * The composer's 模式 button (iOS SessionModeBar): permission mode and effort change on the running session; for
 * Claude in a terminal pane the model too, which goes out as `/model <alias>` ([onPickModel], the same path as the
 * quick messages). Claude's sessions get it (callers check [canChangeModes]), and a Codex thread on its app-server
 * with its effort alone, among its own levels ([canChangeEffortOnly]); all-allow, which a running session cannot pick,
 * stays listed and marked while it is on.
 */
@Composable
internal fun ChatModeBar(
    session: Session?, state: ChatChromeState, online: Boolean, onOverlay: (Boolean) -> Unit = {},
    onPickModel: ((String) -> Unit)? = null,
) {
    var open by remember { mutableStateOf(false) }
    LaunchedEffect(open) { onOverlay(open) }
    DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    val effortOnly = session?.canChangeEffortOnly == true
    val editable = session != null && state.available && online && (session.canChangeModes || effortOnly)
    val bypass = session?.permissionMode == SessionModes.BYPASS
    JIconButton(PhIcons.SlidersHorizontal, if (bypass) "模式：全部放行" else "模式", { open = true },
        tint = if (bypass) PersonaTheme.palette.danger.dot else null)
    if (open) JSheet(onDismiss = { open = false }, title = "对话模式", scrollable = true) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (!editable) Text(if (session?.canChangeModes == false && !effortOnly) "这个会话不能在手机上调整模式" else "连上 hub 后才能调整模式",
                style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            if (session != null) {
                if (!effortOnly) {
                    JSectionHeader("文件权限")
                    SessionModes.switchable(session.permissionMode).forEach { option ->
                        val current = option.id == session.permissionMode
                        DecisionOption(option.title, {
                            open = false
                            if (!current) state.run(session, ChatAction.Modes(permissionMode = option.id))
                        }, detail = option.detail, selected = current, enabled = editable)
                    }
                }
                JSectionHeader("思考强度")
                SessionModes.effortOptions(session).forEach { option ->
                    JRow(option.title, subtitle = option.detail, trailing = { JRadio(option.id == session.effort, null) }, enabled = editable,
                        onClick = { open = false; if (option.id != session.effort) state.run(session, ChatAction.Modes(effort = option.id)) })
                }
                // /model types into Claude's pane: Claude in a terminal engine only (Codex / TRAEX pick in a menu the phone can't drive).
                if (onPickModel != null && session.canTypeClaudeCommands) {
                    val current = SessionModes.claudeModelId(session.model)
                    JSectionHeader("模型")
                    SessionModes.claudeModels.forEach { option ->
                        JRow(option.title, subtitle = option.detail, trailing = { JRadio(option.id == current, null) }, enabled = editable,
                            onClick = { open = false; if (option.id != current) onPickModel(option.id) })
                    }
                    Text("发出 /model 后由电脑上的 Claude 切换；对话已有缓存时它可能先问一句，会显示在这里等你确认。",
                        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
                }
            }
        }
    }
}
