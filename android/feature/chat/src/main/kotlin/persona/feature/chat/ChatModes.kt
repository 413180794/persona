package persona.feature.chat

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.annotation.StringRes
import android.content.res.Resources
import androidx.compose.material3.Text
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.runtime.*
import persona.core.contracts.Session
import persona.core.contracts.api.ChatAction
import persona.core.contracts.api.modelName
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import persona.core.domain.sort.canChangeEffortOnly
import persona.core.domain.sort.canChangeModes
import persona.core.domain.sort.canTypeClaudeCommands

/** Claude Code's permission modes, effort levels and /model aliases as the app shows them (iOS SessionModes, node/modes.go). */
internal object SessionModes {
    /** [id] is what the hub and the agent use; the words are resources, or [raw] for an id the app does not know (shown as is). */
    data class Option(val id: String, @StringRes val titleRes: Int, @StringRes val detailRes: Int, val raw: String? = null) {
        fun title(resources: Resources): String = raw ?: resources.getString(titleRes)
        fun detail(resources: Resources): String = if (detailRes == 0) "" else resources.getString(detailRes)
    }

    const val BYPASS = "bypassPermissions"

    val permissionModes = listOf(
        Option("default", R.string.chat_modes_default, R.string.chat_modes_default_detail),
        Option("acceptEdits", R.string.chat_modes_accept_edits, R.string.chat_modes_accept_edits_detail),
        Option("plan", R.string.chat_modes_plan, R.string.chat_modes_plan_detail),
        Option("auto", R.string.chat_modes_auto, R.string.chat_modes_auto_detail),
        Option(BYPASS, R.string.chat_modes_bypass, R.string.chat_modes_bypass_detail),
    )

    val effortLevels = listOf(
        Option("low", R.string.chat_modes_effort_low, R.string.chat_modes_effort_low_detail),
        Option("medium", R.string.chat_modes_effort_medium, R.string.chat_modes_effort_medium_detail),
        Option("high", R.string.chat_modes_effort_high, R.string.chat_modes_effort_high_detail),
        Option("xhigh", R.string.chat_modes_effort_xhigh, R.string.chat_modes_effort_xhigh_detail),
        Option("max", R.string.chat_modes_effort_max, R.string.chat_modes_effort_max_detail),
    )

    /** Codex's reasoning efforts (node/codex_effort.go, iOS SessionModes.codexEffortLevels); a thread offers its model's. */
    val codexEffortLevels = listOf(
        Option("minimal", R.string.chat_modes_effort_minimal, R.string.chat_modes_codex_minimal_detail),
        Option("low", R.string.chat_modes_effort_low, R.string.chat_modes_codex_low_detail),
        Option("medium", R.string.chat_modes_effort_medium, R.string.chat_modes_codex_medium_detail),
        Option("high", R.string.chat_modes_effort_high, R.string.chat_modes_codex_high_detail),
        Option("xhigh", R.string.chat_modes_effort_xhigh, R.string.chat_modes_effort_xhigh_detail),
        Option("max", R.string.chat_modes_effort_max, R.string.chat_modes_effort_max_detail),
        Option("ultra", R.string.chat_modes_effort_ultra, R.string.chat_modes_codex_ultra_detail),
    )

    /** A running session's effort choices: its own levels when the hub lists them (a Codex thread on its app-server), Claude Code's otherwise. */
    fun effortOptions(session: Session): List<Option> =
        if (session.efforts.isEmpty()) effortLevels
        else session.efforts.map { id -> codexEffortLevels.firstOrNull { it.id == id } ?: Option(id, 0, 0, raw = id) }

    /** Claude Code's `/model` aliases (iOS PromptItems.claudeModels): picking one sends `/model <alias>`. */
    val claudeModels = listOf(
        Option("default", R.string.chat_modes_model_default, R.string.chat_modes_model_default_detail),
        Option("opus", 0, R.string.chat_modes_model_opus_detail, raw = "Opus"),
        Option("sonnet", 0, R.string.chat_modes_model_sonnet_detail, raw = "Sonnet"),
        Option("haiku", 0, R.string.chat_modes_model_haiku_detail, raw = "Haiku"),
    )

    /**
     * What a running session can switch to: not bypassPermissions (only at start), plus the current mode when it is
     * none of these (bypassPermissions, or a newer Claude Code's mode, shown by its raw name), so it still shows as picked.
     */
    fun switchable(current: String?): List<Option> {
        val list = permissionModes.filter { it.id != BYPASS }
        if (current.isNullOrEmpty() || list.any { it.id == current }) return list
        return list + (permissionModes.firstOrNull { it.id == current } ?: Option(current, 0, R.string.chat_modes_unknown_detail, raw = current))
    }

    /** The alias a session's model reads as ("claude-opus-…" → opus); a gateway model or none is the default. */
    fun claudeModelId(model: String?): String {
        val name = model.orEmpty().lowercase()
        return listOf("opus", "sonnet", "haiku").firstOrNull { it in name } ?: "default"
    }

    fun modeLabel(id: String?, resources: Resources): String? =
        id?.let { mode -> permissionModes.firstOrNull { it.id == mode }?.title(resources) ?: mode }
    fun effortLabel(id: String?, resources: Resources): String? = id?.let { effort ->
        (effortLevels.firstOrNull { it.id == effort } ?: codexEffortLevels.firstOrNull { it.id == effort })?.title(resources) ?: effort
    }
}

/** Non-default settings and the current GPT model in the existing hint above the composer. */
internal fun chatModeHint(session: Session?, resources: Resources, includeModes: Boolean = true): String? = listOfNotNull(
    session?.permissionMode?.takeIf { includeModes && it != "default" }?.let { SessionModes.modeLabel(it, resources) },
    session?.effort?.takeIf { includeModes && it != "high" }?.let {
        resources.getString(R.string.chat_modes_effort_hint, SessionModes.effortLabel(it, resources))
    },
    // Only GPT ids get a model hint: other models keep the composer's existing presentation.
    session?.model?.let { id -> modelName(id).takeUnless { it == id } },
).takeIf { it.isNotEmpty() }?.joinToString(resources.getString(R.string.chat_modes_hint_separator))

@Composable
internal fun chatModeHint(session: Session?, includeModes: Boolean = true): String? =
    chatModeHint(session, LocalResources.current, includeModes)

/**
 * The composer's "Mode" button (iOS SessionModeBar): permission mode and effort change on the running session; for
 * Claude in a terminal pane the model too, which goes out as `/model <alias>` ([onPickModel], the same path as the
 * quick messages). Claude's sessions get it (callers check [canChangeModes]), and a Codex thread on its app-server
 * with its effort alone, among its own levels ([canChangeEffortOnly]); all-allow, which a running session cannot pick,
 * stays listed and marked while it is on.
 */
@Composable
internal fun ChatModeBar(
    session: Session?, state: ChatChromeState, online: Boolean, onOverlay: (Boolean) -> Unit = {},
    onPickModel: ((String) -> Unit)? = null,
    /**
     * /model in the "/" list asked for the sheet, where the model is picked: opened, then [onOpenHandled] clears the
     * request, so the bar coming back (after a question card hid the composer) does not open it again.
     */
    openRequested: Boolean = false,
    onOpenHandled: () -> Unit = {},
) {
    var open by remember { mutableStateOf(false) }
    LaunchedEffect(openRequested) { if (openRequested) { open = true; onOpenHandled() } }
    val resources = LocalResources.current
    LaunchedEffect(open) { onOverlay(open) }
    DisposableEffect(Unit) { onDispose { onOverlay(false) } }
    val effortOnly = session?.canChangeEffortOnly == true
    val editable = session != null && state.available && online && (session.canChangeModes || effortOnly)
    val bypass = session?.permissionMode == SessionModes.BYPASS
    JIconButton(PhIcons.SlidersHorizontal, stringResource(if (bypass) R.string.chat_modes_button_bypass else R.string.chat_modes_button), { open = true },
        tint = if (bypass) PersonaTheme.palette.danger.dot else null)
    if (open) JSheet(onDismiss = { open = false }, title = stringResource(R.string.chat_modes_title), scrollable = true) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            if (!editable) Text(stringResource(if (session?.canChangeModes == false && !effortOnly) R.string.chat_modes_locked else R.string.chat_modes_offline),
                style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink2)
            if (session != null) {
                if (!effortOnly) {
                    JSectionHeader(stringResource(R.string.chat_modes_section_permissions))
                    SessionModes.switchable(session.permissionMode).forEach { option ->
                        val current = option.id == session.permissionMode
                        DecisionOption(option.title(resources), {
                            open = false
                            if (!current) state.run(session, ChatAction.Modes(permissionMode = option.id))
                        }, detail = option.detail(resources), selected = current, enabled = editable)
                    }
                }
                JSectionHeader(stringResource(R.string.chat_modes_section_effort))
                SessionModes.effortOptions(session).forEach { option ->
                    JRow(option.title(resources), subtitle = option.detail(resources), trailing = { JRadio(option.id == session.effort, null) }, enabled = editable,
                        onClick = { open = false; if (option.id != session.effort) state.run(session, ChatAction.Modes(effort = option.id)) })
                }
                // /model types into Claude's pane: Claude in a terminal engine only (Codex / TRAEX pick in a menu the phone can't drive).
                if (onPickModel != null && session.canTypeClaudeCommands) {
                    val current = SessionModes.claudeModelId(session.model)
                    JSectionHeader(stringResource(R.string.chat_modes_section_model))
                    SessionModes.claudeModels.forEach { option ->
                        JRow(option.title(resources), subtitle = option.detail(resources), trailing = { JRadio(option.id == current, null) }, enabled = editable,
                            onClick = { open = false; if (option.id != current) onPickModel(option.id) })
                    }
                    Text(stringResource(R.string.chat_modes_model_note),
                        style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3)
                }
            }
        }
    }
}
