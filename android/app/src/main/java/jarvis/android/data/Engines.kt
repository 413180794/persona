package jarvis.android.data

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import kotlinx.coroutines.CancellationException

/**
 * One terminal engine per machine (docs/engines.md), as on iOS `EngineSelectionModel`. The options are what the hub
 * reports in `engines`; [Engines.known] only names the engines this build knows, for older hubs that report nothing
 * and to decide what may be chosen without detection (offline machines).
 */
object Engines {
    const val DEFAULT = "herdr"

    /** An engine this build knows: its id, a fallback name, and a line on where its sessions live. */
    data class Known(val id: String, val name: String, val detail: String)

    val known = listOf(
        Known("herdr", "herdr", "终端里的 herdr"),
        Known("herdr-core", "Herdr App", "桌面 App 里开的终端"),
        Known("orca", "Orca", "Orca 里的终端"),
    )

    fun detail(id: String): String? = known.firstOrNull { it.id == id }?.detail

    /** The chosen engine, herdr when the hub sent none. */
    fun current(m: Machine): String = m.engine.ifBlank { DEFAULT }

    /** The name to show for the chosen engine: the hub's name for it, else the known one, else the id. */
    fun currentName(m: Machine): String {
        val id = current(m)
        return m.engines.firstOrNull { it.id == id }?.name?.takeIf { it.isNotBlank() }
            ?: known.firstOrNull { it.id == id }?.name ?: id
    }

    /** The machine pairing asks about: the hub's own (kind local), else the first one. */
    fun pairingMachine(machines: List<Machine>): Machine? = machines.firstOrNull { it.kind == "local" } ?: machines.firstOrNull()

    /** What the picker starts on: the machine's current engine, herdr when never chosen (2026-11-01 user: an engine
     *  must be picked, herdr by default; no guessing from what happens to be detected). */
    @Suppress("UNUSED_PARAMETER")
    fun initialChoice(m: Machine, pairing: Boolean): String = current(m)

    /** The rows to list: the hub's `engines` as given; with none reported, the known engines (not detected). */
    fun options(m: Machine): List<DetectedEngine> =
        m.engines.ifEmpty { known.map { DetectedEngine(it.id, it.name, available = false) } }

    /** Known engines can be set without detection (offline: applied on connect); an unknown one only when the hub found it. */
    fun canSelect(id: String, m: Machine): Boolean =
        known.any { it.id == id } || m.engines.any { it.id == id && it.available }

    /** The detection line under an option; missing detection is not reported as 没检测到. */
    fun detection(id: String, m: Machine): String {
        if (!m.online) return "离线，暂无检测信息"
        val found = m.engines.firstOrNull { it.id == id } ?: return "暂无检测信息"
        val status = if (found.available) "检测到可用" else "没检测到"
        return if (found.available && !found.version.isNullOrBlank()) "$status · ${found.version}" else status
    }

    /** The explanation under the options. */
    fun footer(m: Machine): String =
        if (m.online) "只显示所选引擎里的会话，随时可在设置 › 电脑 › 机器里修改。"
        else "机器离线，选择会在连接后生效。只显示所选引擎里的会话。"
}

/**
 * The picker's state: the choice stays local until the hub accepts the PUT. [save] goes through [put]; the machine it
 * returns goes to [updated] (the hub's list) so the page shows it before the `machines` push.
 */
class EngineSelection(
    machine: Machine,
    private val pairing: Boolean,
    private val put: suspend (machineId: String, engine: String) -> Machine,
    private val updated: (Machine) -> Unit,
) {
    var selected by mutableStateOf(Engines.initialChoice(machine, pairing))
        private set
    var saving by mutableStateOf(false)
        private set
    var error by mutableStateOf<String?>(null)
        private set
    private var edited = false

    fun choose(id: String) {
        selected = id
        edited = true
        error = null
    }

    /** Live detection and current-engine changes move the picker until the user starts editing it. */
    fun synchronize(machine: Machine) {
        if (edited || saving) return
        selected = Engines.initialChoice(machine, pairing)
    }

    /** True when the hub took the choice; false with [error] set otherwise (pairing can still go on). */
    suspend fun save(machineId: String): Boolean {
        if (saving) return false
        saving = true
        error = null
        return try {
            val saved = put(machineId, selected)
            updated(saved)
            selected = Engines.current(saved)
            edited = false
            true
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            val why = when (e) {
                is HubException -> e.message.ifBlank { "hub 返回 ${e.status}" }
                is java.io.IOException -> "连不上 hub"
                else -> e.message ?: e.javaClass.simpleName
            }
            error = "保存失败：$why"
            false
        } finally {
            saving = false
        }
    }
}

/**
 * The engine step right after pairing (QR, manual, link): the machine to ask about, captured from the pairing check,
 * so the step can show before the new hub's first `machines` push. Null when there is no step to show.
 */
object PairingEngine {
    val pending = mutableStateOf<Machine?>(null)
}
