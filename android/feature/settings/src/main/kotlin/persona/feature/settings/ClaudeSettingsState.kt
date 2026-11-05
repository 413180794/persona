package persona.feature.settings

import android.content.Context
import persona.core.contracts.Machine
import persona.core.contracts.api.ClaudeSetting
import persona.core.contracts.api.MachineManagement
import persona.core.designsystem.failureReason
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.serialization.json.JsonElement

/** The machine last looked at and whether changes go to every online machine; kept on this phone. */
internal interface ClaudeSettingsChoices {
    var machine: String
    var applyAll: Boolean
}

internal class StoredClaudeSettingsChoices(context: Context) : ClaudeSettingsChoices {
    private val preferences = context.applicationContext.getSharedPreferences("claude-settings", Context.MODE_PRIVATE)
    override var machine: String
        get() = preferences.getString("machine", "").orEmpty()
        set(value) { preferences.edit().putString("machine", value).apply() }
    override var applyAll: Boolean
        get() = preferences.getBoolean("apply-all", true)
        set(value) { preferences.edit().putBoolean("apply-all", value).apply() }
}

internal data class ClaudeSettingsState(
    val machineId: String = "",
    val applyAll: Boolean = true,
    val settings: List<ClaudeSetting> = emptyList(),
    val loading: Boolean = false,
    val error: ResText? = null,
)

/** First visit: the hub's own machine; a remembered one that went offline falls back to the first online machine. */
internal fun claudeSettingsMachine(remembered: String, online: List<Machine>): String? =
    remembered.takeIf { id -> online.any { it.id == id } } ?: (online.firstOrNull { it.kind == "local" } ?: online.firstOrNull())?.id

/**
 * 设置 › Claude Code 设置 (iOS `ClaudeSettingsView`): the whitelisted `~/.claude/settings.json` options of one
 * machine, written to every online machine at once unless that is switched off.
 */
internal class ClaudeSettingsModel(private val api: MachineManagement, private val choices: ClaudeSettingsChoices) {
    private val mutable = MutableStateFlow(ClaudeSettingsState(applyAll = choices.applyAll))
    val state = mutable.asStateFlow()

    /** Keeps the shown machine among the [online] ones (see [claudeSettingsMachine]); none while nothing is online. */
    fun follow(online: List<Machine>) {
        val pick = claudeSettingsMachine(state.value.machineId.ifBlank { choices.machine }, online)
        if (pick == null) mutable.update { it.copy(machineId = "", settings = emptyList(), error = null) } else choose(pick)
    }

    fun choose(machineId: String) {
        choices.machine = machineId
        mutable.update { if (it.machineId == machineId) it else it.copy(machineId = machineId, settings = emptyList(), error = null) }
    }

    fun applyAll(value: Boolean) {
        choices.applyAll = value
        mutable.update { it.copy(applyAll = value) }
    }

    /** Reads the shown machine's settings; [keepError] leaves a write's failure on screen. */
    suspend fun load(keepError: Boolean = false) {
        val machine = state.value.machineId.takeIf { it.isNotBlank() } ?: return
        mutable.update { it.copy(loading = true) }
        try {
            val settings = api.claudeSettings(machine)
            mutable.update { if (it.machineId == machine) it.copy(settings = settings, error = it.error.takeIf { keepError }) else it }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { if (it.machineId == machine) it.copy(error = ResText.of(R.string.settings_src_claude_read_failed, failureReason(error))) else it } }
        finally { mutable.update { it.copy(loading = false) } }
    }

    /**
     * Writes [key] (JsonNull: back to the default) to this machine, or to every [online] machine; the control moves
     * at once and the hub's answer (or a reload after a failure) corrects it. Returns the notice to show when it worked.
     */
    suspend fun set(key: String, value: JsonElement, online: List<Machine>): ResText? {
        val current = state.value
        val targets = if (current.applyAll) online.map { it.id }.ifEmpty { listOf(current.machineId) } else listOf(current.machineId)
        mutable.update { s -> s.copy(settings = s.settings.map { if (it.key == key) it.copy(value = value) else it }) }
        val failed = mutableListOf<ResText>()
        for (id in targets) {
            try {
                val list = api.setClaudeSetting(id, key, value)
                if (id == state.value.machineId) mutable.update { it.copy(settings = list) }
            } catch (cancelled: CancellationException) { throw cancelled }
            catch (error: Exception) { failed += ResText.of(R.string.settings_src_name_reason, online.firstOrNull { it.id == id }?.displayName ?: id, failureReason(error)) }
        }
        if (failed.isEmpty()) {
            mutable.update { it.copy(error = null) }
            return if (targets.size > 1) ResText.plural(R.plurals.settings_src_claude_applied, targets.size, targets.size) else ResText.of(R.string.settings_src_saved)
        }
        mutable.update { it.copy(error = ResText.of(R.string.settings_src_claude_not_all_saved, failed.toList())) }
        load(keepError = true)
        return null
    }
}
