package jarvis.feature.assistant.settings

import android.content.Context
import jarvis.core.contracts.Machine
import jarvis.core.contracts.api.AgentLauncher
import jarvis.core.contracts.api.AssistantConfig
import jarvis.core.contracts.api.AssistantConfigUpdate
import jarvis.core.contracts.api.AssistantManagement
import jarvis.core.contracts.api.MemoryFile
import jarvis.core.contracts.parseTime
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.time.Instant
import java.time.ZoneId
import java.util.Locale
import java.util.TimeZone

/** The calls 设置 › 助手 makes: narrower than [AssistantManagement], so it stays put while that one grows. */
internal interface AssistantSettingsApi {
    suspend fun config(): AssistantConfig
    suspend fun updateConfig(update: AssistantConfigUpdate): AssistantConfig
    suspend fun launchers(machineId: String): List<AgentLauncher>
    suspend fun memory(): List<MemoryFile>
    suspend fun saveMemory(name: String, content: String)
    suspend fun deleteMemory(name: String)
}

internal fun AssistantManagement.settingsApi(): AssistantSettingsApi {
    val management = this
    return object : AssistantSettingsApi {
        override suspend fun config() = management.config()
        override suspend fun updateConfig(update: AssistantConfigUpdate) = management.updateConfig(update)
        override suspend fun launchers(machineId: String) = management.launchers(machineId)
        override suspend fun memory() = management.memory()
        override suspend fun saveMemory(name: String, content: String) = management.saveMemory(name, content)
        override suspend fun deleteMemory(name: String) = management.deleteMemory(name)
    }
}

/** Whether the hub's time zone follows this phone's (the default, as on iOS); the choice stays on the phone. */
internal interface TimezoneChoice {
    val phoneZone: String
    var followPhone: Boolean
}

internal class StoredTimezoneChoice(context: Context) : TimezoneChoice {
    private val preferences = context.applicationContext.getSharedPreferences("jarvis-assistant", Context.MODE_PRIVATE)
    override val phoneZone: String get() = TimeZone.getDefault().id
    override var followPhone: Boolean
        get() = preferences.getBoolean("timezone.follow_phone", true)
        set(value) { preferences.edit().putBoolean("timezone.follow_phone", value).apply() }
}

/** The zone to send so the hub follows the phone; null when it already does or the user picked one. */
internal fun zoneToSync(choice: TimezoneChoice, hubZone: String?): String? =
    choice.phoneZone.takeIf { choice.followPhone && it.isNotBlank() && it != hubZone }

/** Routines and the assistant's sense of "today" follow the phone's zone unless the user picked another one. */
internal suspend fun followPhoneZone(api: AssistantSettingsApi, choice: TimezoneChoice, known: AssistantConfig? = null): AssistantConfig {
    val config = known ?: api.config()
    val zone = zoneToSync(choice, config.timezone) ?: return config
    return api.updateConfig(AssistantConfigUpdate(timezone = zone))
}

/** Moving the assistant to another machine, waiting for the user to confirm. */
internal data class MachineMove(val fromName: String, val to: Machine) {
    val title: String get() = "换到「${to.displayName}」上运行？"
    val text: String get() = machineMoveWarning(fromName, to.displayName, to.online)
}

/** Conversations and memory are files on the machine the assistant runs on: they never move with it. */
internal fun machineMoveWarning(from: String, to: String, online: Boolean): String = buildString {
    append("记忆和会话记录在原来那台机器上，不会跟过去。")
    append("换过去以后，Jarvis 在「$to」上从头开始：没有之前的记忆，主对话也重新开始；原来的记录留在「$from」上，换回来就还在。")
    if (!online) append("\n\n「$to」现在离线，连上之前 Jarvis 不能回复。")
}

internal data class SettingsNotice(val text: String, val error: Boolean)

internal data class MemoryState(
    val entries: List<MemoryEntry>? = null,
    val index: MemoryFile? = null,
    val loading: Boolean = false,
    val failed: Boolean = false,
)

internal data class AssistantSettingsUiState(
    val config: AssistantConfig? = null,
    val loading: Boolean = false,
    val loadFailed: Boolean = false,
    val busy: Boolean = false,
    val notice: SettingsNotice? = null,
    /** The Claude launchers on the assistant's machine; null until read. */
    val launchers: List<AgentLauncher>? = null,
    val launchersLoading: Boolean = false,
    val launchersFailed: Boolean = false,
    val move: MachineMove? = null,
    val memory: MemoryState = MemoryState(),
    val followPhone: Boolean = true,
) {
    /** The saved launcher is not one the machine offers: new conversations would fail to start. */
    val launcherMissing: Boolean get() = config != null && launchers != null && launchers.isNotEmpty() &&
        launchers.none { it.name == config.launcher }
}

/** Mutations run one at a time; every outcome lands in [state] (the page shows it, nothing is thrown). */
internal class AssistantSettingsState(
    private val api: AssistantSettingsApi,
    private val timezone: TimezoneChoice,
    /** After a move: the conversation list now comes from the other machine. */
    private val onMoved: suspend () -> Unit = {},
) {
    private val mutable = MutableStateFlow(AssistantSettingsUiState(followPhone = timezone.followPhone))
    val state = mutable.asStateFlow()
    val phoneZone: String get() = timezone.phoneZone

    private fun set(change: AssistantSettingsUiState.() -> AssistantSettingsUiState) = mutable.update(change)

    fun clearNotice() = set { copy(notice = null) }

    suspend fun load() {
        set { copy(loading = true, loadFailed = false) }
        try {
            val config = api.config()
            set { copy(config = config) }
            // Like iOS: opening the page brings the hub's zone along with the phone's.
            quietly { val synced = followPhoneZone(api, timezone, config); set { copy(config = synced) } }
        } catch (error: CancellationException) { throw error }
        catch (_: Exception) { set { copy(loadFailed = true) } }
        finally { set { copy(loading = false) } }
    }

    suspend fun loadLaunchers() {
        val machine = state.value.config?.machineId?.takeIf { it.isNotBlank() } ?: return
        set { copy(launchersLoading = true, launchersFailed = false) }
        try {
            // The assistant is headless Claude Code (claude -p): Codex and TRAEX launchers cannot run it.
            val claude = api.launchers(machine).filter { it.agentType == "claude" }
            if (state.value.config?.machineId == machine) set { copy(launchers = claude) }
        } catch (error: CancellationException) { throw error }
        catch (_: Exception) { if (state.value.config?.machineId == machine) set { copy(launchers = null, launchersFailed = true) } }
        finally { set { copy(launchersLoading = false) } }
    }

    fun requestMove(to: Machine, machines: List<Machine>) {
        val config = state.value.config ?: return
        if (state.value.busy || to.id == config.machineId) return
        val from = machines.firstOrNull { it.id == config.machineId }?.displayName ?: config.machineId.ifBlank { "原来那台机器" }
        set { copy(move = MachineMove(from, to)) }
    }

    fun cancelMove() = set { copy(move = null) }

    suspend fun confirmMove() {
        val move = state.value.move ?: return
        set { copy(move = null) }
        val moved = perform("没换成，Jarvis 还在「${move.fromName}」上运行，请重试。") {
            // The main conversation is a transcript on the old machine; the next message starts a new one here.
            val config = api.updateConfig(AssistantConfigUpdate(machineId = move.to.id, main = AssistantConfigUpdate.CLEAR_MAIN))
            set { copy(config = config, launchers = null, launchersFailed = false, memory = MemoryState()) }
            done("Jarvis 换到「${move.to.displayName}」上了，新对话从这里开始。")
        }
        if (!moved) return
        quietly { onMoved() }
        loadLaunchers()
        loadMemory()
    }

    suspend fun chooseLauncher(name: String) {
        val config = state.value.config ?: return
        if (name.isBlank() || name == config.launcher) return
        perform("启动器没换成，请重试。") {
            val updated = api.updateConfig(AssistantConfigUpdate(launcher = name))
            set { copy(config = updated) }
            done("新对话将使用 $name")
        }
    }

    suspend fun setFollowPhone(follow: Boolean) {
        if (state.value.busy || follow == state.value.followPhone) return
        timezone.followPhone = follow
        set { copy(followPhone = follow) }
        if (follow) perform("时区没同步上，请重试。") {
            val synced = followPhoneZone(api, timezone, state.value.config)
            set { copy(config = synced) }
        }
    }

    suspend fun chooseZone(zone: String) {
        if (state.value.followPhone || zone.isBlank() || zone == state.value.config?.timezone) return
        perform("时区没改成，请重试。") {
            val updated = api.updateConfig(AssistantConfigUpdate(timezone = zone))
            set { copy(config = updated) }
            done("定时任务改按 $zone 的时刻运行")
        }
    }

    suspend fun saveNotes(text: String): Boolean = perform("没保存上，修改的内容还在，请重试。") {
        val updated = api.updateConfig(AssistantConfigUpdate(notes = text))
        set { copy(config = updated) }
        done("已保存，新对话起生效")
    }

    suspend fun loadMemory() {
        set { copy(memory = memory.copy(loading = true, failed = false)) }
        try {
            val (entries, index) = memoryEntries(api.memory())
            set { copy(memory = MemoryState(entries, index)) }
        } catch (error: CancellationException) { throw error }
        catch (_: Exception) { set { copy(memory = memory.copy(loading = false, failed = true)) } }
    }

    suspend fun saveMemory(name: String, content: String): Boolean {
        val saved = perform("没保存上，修改的内容还在，请重试。") {
            api.saveMemory(name, content)
            // Show the edit right away; the reload below picks up anything else that changed meanwhile.
            set {
                val files = memory.entries.orEmpty().map { it.file } + listOfNotNull(memory.index)
                val (entries, index) = memoryEntries(files.map { if (it.name == name) it.copy(content = content) else it })
                copy(memory = memory.copy(entries = entries, index = index))
            }
            done("记忆已更新")
        }
        if (saved) loadMemory()
        return saved
    }

    suspend fun deleteMemory(name: String): Boolean {
        val deleted = perform("没删掉，请重试。") {
            api.deleteMemory(name)
            set { copy(memory = memory.copy(entries = memory.entries?.filterNot { it.file.name == name })) }
            done("已删除这条记忆")
        }
        if (deleted) loadMemory()
        return deleted
    }

    private fun done(text: String) = set { copy(notice = SettingsNotice(text, error = false)) }

    private suspend fun perform(failure: String, block: suspend () -> Unit): Boolean {
        if (state.value.busy) return false
        set { copy(busy = true, notice = null) }
        return try { block(); true }
        catch (error: CancellationException) { throw error }
        catch (_: Exception) { set { copy(notice = SettingsNotice(failure, error = true)) }; false }
        finally { set { copy(busy = false) } }
    }

    private suspend fun quietly(block: suspend () -> Unit) {
        try { block() } catch (error: CancellationException) { throw error } catch (_: Exception) { }
    }
}

internal const val MEMORY_INDEX = "MEMORY.md"

/** One memory file: its front matter (`---` … `---`: name, description, type) and the text after it. */
internal data class MemoryEntry(
    val file: MemoryFile,
    val title: String,
    val description: String?,
    val type: String?,
    val body: String,
) {
    val typeLabel: String? get() = when (type) {
        null -> null
        "user" -> "用户"
        "feedback" -> "反馈"
        "project" -> "项目"
        "reference" -> "参考"
        else -> type
    }
    val updated: Instant? get() = parseTime(file.updatedAt)
}

/** The memory files newest first, titled from the index where it has a line for them, and the index itself. */
internal fun memoryEntries(files: List<MemoryFile>): Pair<List<MemoryEntry>, MemoryFile?> {
    val index = files.firstOrNull { it.name == MEMORY_INDEX }
    val titles = index?.let { memoryIndexLines(it.content) }.orEmpty()
    val entries = files.filter { it.name != MEMORY_INDEX }.map { parseMemory(it, titles[it.name]) }
        .sortedWith(compareByDescending<MemoryEntry> { it.updated ?: Instant.EPOCH }.thenBy { it.file.name })
    return entries to index
}

/** The index's lines `- [标题](file.md) — 说明`, by file. */
internal fun memoryIndexLines(content: String): Map<String, Pair<String, String?>> = buildMap {
    for (line in content.lines()) {
        val open = line.indexOf('[').takeIf { it >= 0 } ?: continue
        val close = line.indexOf("](", open).takeIf { it > open } ?: continue
        val paren = line.indexOf(')', close + 2).takeIf { it > close } ?: continue
        val title = line.substring(open + 1, close).trim()
        val file = line.substring(close + 2, paren).trim()
        val hook = line.substring(paren + 1).trimStart(' ', '—', '–', '-', ':').trim()
        if (title.isNotEmpty() && file.isNotEmpty()) put(file, title to hook.ifEmpty { null })
    }
}

internal fun parseMemory(file: MemoryFile, indexed: Pair<String, String?>? = null): MemoryEntry {
    val lines = file.content.lines()
    val end = if (lines.firstOrNull()?.trim() == "---") lines.drop(1).indexOfFirst { it.trim() == "---" } + 1 else 0
    val fields = mutableMapOf<String, String>()
    if (end > 0) for (line in lines.subList(1, end)) {
        val colon = line.indexOf(':').takeIf { it > 0 } ?: continue
        // `type` sits under `metadata:` (indented) in newer files and at the top level in older ones.
        val key = line.substring(0, colon).trim()
        var value = line.substring(colon + 1).trim()
        if (value.length >= 2 && (value.first() == '"' || value.first() == '\'') && value.last() == value.first())
            value = value.substring(1, value.length - 1)
        if (value.isNotEmpty() && key in setOf("name", "description", "type")) fields.putIfAbsent(key, value)
    }
    val body = (if (end > 0) lines.drop(end + 1) else lines).joinToString("\n").trim()
    return MemoryEntry(
        file, title = indexed?.first ?: fields["name"] ?: file.name.removeSuffix(".md"),
        description = fields["description"] ?: indexed?.second, type = fields["type"], body = body,
    )
}

/** `~/.jarvis/assistant` rather than the full path, when it is under the machine's home. */
internal fun homeRelative(path: String, home: String?): String {
    val root = home?.trimEnd('/')?.takeIf { it.isNotEmpty() } ?: return path
    return when {
        path == root -> "~"
        path.startsWith("$root/") -> "~" + path.removePrefix(root)
        else -> path
    }
}

/** "UTC+8", "UTC+5:30", "UTC" for a zone right now; null for an id Java does not know. */
internal fun zoneOffset(zone: String, at: Instant = Instant.now()): String? = runCatching {
    val seconds = ZoneId.of(zone).rules.getOffset(at).totalSeconds
    if (seconds == 0) return@runCatching "UTC"
    val sign = if (seconds < 0) "-" else "+"
    val hours = Math.abs(seconds) / 3600
    val minutes = Math.abs(seconds) % 3600 / 60
    "UTC$sign$hours" + if (minutes != 0) String.format(Locale.ROOT, ":%02d", minutes) else ""
}.getOrNull()

/** The zone's Chinese name ("中国标准时间"), when the system has one. */
internal fun zoneName(zone: String): String? = runCatching {
    TimeZone.getTimeZone(ZoneId.of(zone)).getDisplayName(Locale.SIMPLIFIED_CHINESE).takeIf { it.isNotBlank() && !it.startsWith("GMT") }
}.getOrNull()

/** Region zones (Asia/Shanghai …) by offset, then name: what the picker lists. */
internal fun pickableZones(at: Instant = Instant.now()): List<String> = ZoneId.getAvailableZoneIds()
    .filter { '/' in it && !it.startsWith("Etc/") && !it.startsWith("SystemV/") && it.first().isUpperCase() }
    .sortedWith(compareBy<String> { runCatching { ZoneId.of(it).rules.getOffset(at).totalSeconds }.getOrDefault(0) }.thenBy { it })

internal fun zoneMatches(zone: String, query: String): Boolean {
    val q = query.trim()
    return q.isEmpty() || zone.contains(q, ignoreCase = true) || zone.replace('_', ' ').contains(q, ignoreCase = true) ||
        zoneName(zone)?.contains(q, ignoreCase = true) == true || zoneOffset(zone)?.contains(q, ignoreCase = true) == true
}
