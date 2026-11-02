package jarvis.android.data

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import java.time.Instant

// Wire types mirroring docs/protocol.md. Every field is optional with a default: hubs add fields over time and an
// older hub leaves newer ones out (the iOS models decode the same way).

val JsonCodec = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    coerceInputValues = true
    encodeDefaults = false
}

/** RFC3339 from the hub; Go's zero time ("0001-01-01T00:00:00Z") means unset. */
fun parseTime(s: String?): Instant? {
    if (s.isNullOrEmpty() || s.startsWith("0001-")) return null
    return runCatching { Instant.parse(s) }.getOrNull()
}

@Serializable
data class Machine(
    val id: String,
    val name: String = "",
    val kind: String = "",
    val icon: String? = null,
    @SerialName("ssh_host") val sshHost: String? = null,
    val online: Boolean = false,
    val error: String? = null,
    val hostname: String? = null,
    val os: String? = null,
    @SerialName("herdr_version") val herdrVersion: String? = null,
    @SerialName("claude_version") val claudeVersion: String? = null,
    @SerialName("last_seen") val lastSeen: String? = null,
    @SerialName("node_mismatch") val nodeMismatch: Boolean? = null,
    /** The hub is installing its own build there (重新部署); deployError: why the last try did not work. */
    val deploying: Boolean? = null,
    @SerialName("deploy_error") val deployError: String? = null,
    val arch: String? = null,
    @SerialName("node_version") val nodeVersion: String? = null,
    /** What the machine runs (docs/machines.md); null from hubs without monitoring. */
    val system: MachineSystem? = null,
    /** The terminal engine chosen for this machine (docs/engines.md); the hub always sends it, herdr when never chosen. */
    val engine: String = "herdr",
    /** What the node found there, as the hub reports it; empty from older hubs / nodes and before the first connect. */
    val engines: List<DetectedEngine> = emptyList(),
) {
    val displayName: String get() = name.ifEmpty { id }
}

/** One engine the node looked for on its machine (docs/protocol.md「引擎」); [name] and [note] are shown as given. */
@Serializable
data class DetectedEngine(
    val id: String,
    val name: String = "",
    val available: Boolean = false,
    val version: String? = null,
    val note: String? = null,
)

/** A machine's OS and hardware as its node reports them (docs/machines.md「系统」). */
@Serializable
data class MachineSystem(
    val os: String? = null,
    val distro: String? = null,
    val like: String? = null,
    val name: String? = null,
    val version: String? = null,
    val build: String? = null,
    val kernel: String? = null,
    val arch: String? = null,
    val model: String? = null,
    val cpu: String? = null,
    val cores: Int? = null,
    val memory: Long? = null,
    @SerialName("boot_time") val bootTime: Long? = null,
)

/** Unified agent state computed by the hub (docs/agent-states.md). */
enum class AgentState(val wire: String, val priority: Int) {
    NeedsYou("needs_you", 0), Unread("unread", 1), Working("working", 2), Idle("idle", 3), Unknown("unknown", 4),
    Offline("offline", 5);

    companion object {
        fun of(wire: String?, status: String?): AgentState =
            entries.firstOrNull { it.wire == wire } ?: when (status) {
                "blocked" -> NeedsYou
                "working" -> Working
                "done" -> Unread
                "idle" -> Idle
                "offline" -> Offline
                else -> Unknown
            }
    }
}

@Serializable
data class LoopInfo(
    val kind: String = "",
    val prompt: String? = null,
    @SerialName("interval_seconds") val intervalSeconds: Int? = null,
    @SerialName("next_at") val nextAt: String? = null,
    val cron: String? = null,
)

@Serializable
data class BackgroundTask(
    val id: String = "",
    val kind: String = "",
    val description: String? = null,
    val command: String? = null,
)

/** An agent the session launched (the Agent tool); only `running` matters to the list. */
@Serializable
data class Subagent(val id: String = "", val running: Boolean = false)

@Serializable
data class SessionCapabilities(
    val read: Boolean = false,
    val send: Boolean = false,
    val interrupt: Boolean = false,
    val approve: Boolean = false,
    val terminal: Boolean = false,
    val attach: Boolean = false,
)

@Serializable
data class Session(
    @SerialName("machine_id") val machineId: String = "",
    @SerialName("session_id") val sessionId: String,
    val title: String = "",
    val cwd: String = "",
    val status: String = "unknown",
    @SerialName("pane_id") val paneId: String? = null,
    val agent: String? = null,
    @SerialName("agent_name") val agentName: String? = null,
    @SerialName("source_kind") val sourceKind: String? = null,
    @SerialName("source_thread_id") val sourceThreadId: String? = null,
    @SerialName("source_instance_id") val sourceInstanceId: String? = null,
    @SerialName("parent_thread_id") val parentThreadId: String? = null,
    @SerialName("read_only_reason") val readOnlyReason: String? = null,
    val capabilities: SessionCapabilities? = null,
    val attached: Boolean = false,
    /** What the hub says to call an agent this app does not know (a runtime's own name, "DSH"; docs/runtimes.md). */
    @SerialName("agent_label") val agentLabelHint: String? = null,
    val launcher: String? = null,
    val model: String? = null,
    @SerialName("permission_mode") val permissionMode: String? = null,
    /** Claude Code's effort level (low … max) as the session runs with it. */
    val effort: String? = null,
    val assistant: Boolean = false,
    @SerialName("workspace_id") val workspaceId: String? = null,
    @SerialName("space_id") val spaceId: String? = null,
    @SerialName("space_name") val spaceName: String? = null,
    @SerialName("space_index") val spaceIndex: Int? = null,
    @SerialName("slot_name") val slotName: String? = null,
    @SerialName("slot_index") val slotIndex: Int? = null,
    val state: String? = null,
    @SerialName("seen_at") val seenAt: String? = null,
    val queued: List<String>? = null,
    val background: List<BackgroundTask>? = null,
    val subagents: List<Subagent>? = null,
    val loop: LoopInfo? = null,
    @SerialName("updated_at") val updatedAt: String? = null,
    @SerialName("last_text") val lastText: String? = null,
    @SerialName("pending_interactions") val pendingInteractions: Int = 0,
    val rev: Long? = null,
    val suggestion: String? = null,
    /** Jarvis is still opening its tab and starting claude (docs/protocol.md「新建会话」): what is sent now goes in once it is ready. */
    val starting: Boolean? = null,
    /** Starting it failed (shown in the chat; sends get the same error). */
    @SerialName("start_error") val startError: String? = null,
) {
    val key: SessionKey get() = SessionKey(machineId, sessionId)
    val agentState: AgentState get() = AgentState.of(state, status)
    val folderName: String get() = cwd.trimEnd('/').substringAfterLast('/')
    val displayTitle: String get() = if (title.isEmpty() || title == "Claude Code") folderName.ifEmpty { sessionId.take(8) } else title
    val hasSource: Boolean get() = !sourceKind.isNullOrEmpty()
    val canRead: Boolean get() = !hasSource || capabilities?.read == true
    val canSend: Boolean get() = !hasSource || capabilities?.send == true
    val canInterrupt: Boolean get() = !hasSource || capabilities?.interrupt == true
    val canApprove: Boolean get() = !hasSource || capabilities?.approve == true
    val canTerminal: Boolean get() = !hasSource || capabilities?.terminal == true
    val canAttach: Boolean get() = hasSource && capabilities?.attach == true && !attached
    val isCodex: Boolean get() = agent == "codex" || sourceKind == "codex_app_server" || sourceKind == "codex_history"
    val sourceLabel: String? get() = when (sourceKind) {
        "codex_app_server" -> "Codex 实时"
        "identity_conflict" -> "身份冲突"
        "codex_history" -> "Codex 历史"
        "runtime" -> agentLabel // an agent runtime's session (docs/runtimes.md): its own name, "DSH"
        else -> sourceKind?.takeIf { it.isNotEmpty() }
    }
    val sourceSubtitle: String? get() = sourceLabel?.let {
        val label = if (sourceKind != "identity_conflict" && !agentName.isNullOrEmpty()) "$it · $agentName" else it
        if (canSend) label else "$label · 仅查看：${readOnlyReason?.takeIf { r -> r.isNotEmpty() } ?: "此来源尚未允许发送"}"
    }
    val agentLabel: String get() = if (isCodex) "Codex" else when (agent) {
        "traex" -> "TRAEX"
        null, "", "claude" -> "Claude"
        else -> agentLabelHint ?: agent.orEmpty()
    }
    val isLive: Boolean get() = status != "offline"
    val updated: Instant? get() = parseTime(updatedAt)
}

data class SessionKey(val machineId: String, val sessionId: String) {
    val path: String get() = "$machineId/$sessionId"
}

@Serializable
data class ImagePayload(
    val mime: String = "image/png",
    val data: String = "",
    val lazy: Boolean? = null,
    val width: Int? = null,
    val height: Int? = null,
) {
    /** The inline base64 bytes, or null when they must be fetched (a lazy placeholder carries `"data":""`). */
    val inlineData: String? get() = data.takeIf { it.isNotEmpty() && lazy != true }
}

@Serializable
data class Item(
    val id: String,
    val seq: Int = 0,
    val ts: String? = null,
    val role: String = "",
    val kind: String = "",
    val text: String? = null,
    @SerialName("tool_use_id") val toolUseId: String? = null,
    @SerialName("tool_name") val toolName: String? = null,
    @SerialName("tool_input") val toolInput: JsonElement? = null,
    @SerialName("is_error") val isError: Boolean? = null,
    @SerialName("full_size") val fullSize: Int? = null,
    val image: ImagePayload? = null,
    val queued: Boolean? = null,
    val from: String? = null,
    val wake: Boolean? = null,
) {
    val time: Instant? get() = parseTime(ts)

    /** The text without the "…（已截断…" line the hub appends where it cut it. */
    val cutText: String
        get() {
            val t = text ?: return ""
            if (fullSize == null) return t
            val i = t.lastIndexOf("\n…（已截断")
            return if (i >= 0) t.substring(0, i) else t
        }

    /** A tool input field as a string ("command", "file_path"…), when it is one. */
    fun inputString(key: String): String? =
        ((toolInput as? kotlinx.serialization.json.JsonObject)?.get(key) as? kotlinx.serialization.json.JsonPrimitive)
            ?.takeIf { it.isString }?.content
}

@Serializable
data class QuestionOption(val label: String = "", val description: String? = null) {
    /** The label without "(Recommended)" (answers send [label] verbatim). */
    val displayLabel: String get() = splitRecommended(label).first
    val recommended: Boolean get() = splitRecommended(label).second

    companion object {
        fun splitRecommended(label: String): Pair<String, Boolean> {
            for (suffix in listOf(" (Recommended)", "(Recommended)", "（推荐）", "(推荐)")) {
                if (label.endsWith(suffix)) return label.dropLast(suffix.length).trim() to true
            }
            return label to false
        }
    }
}

@Serializable
data class Question(
    val question: String = "",
    val header: String? = null,
    @SerialName("multi_select") val multiSelect: Boolean = false,
    val options: List<QuestionOption> = emptyList(),
)

@Serializable
data class DialogOption(val key: String = "", val label: String = "", val detail: String? = null, val kind: String? = null) {
    /** "Type something": the answer is typed text. */
    val needsText: Boolean get() = kind == "type_something"
}

/** A terminal dialog the hub parsed (server/internal/screen); answer with Resolution.option. */
@Serializable
data class ScreenDialog(
    val header: String? = null,
    val question: String = "",
    val detail: String? = null,
    val options: List<DialogOption> = emptyList(),
    val rev: String = "",
)

@Serializable
data class Interaction(
    val id: String,
    @SerialName("machine_id") val machineId: String = "",
    @SerialName("session_id") val sessionId: String = "",
    @SerialName("session_title") val sessionTitle: String? = null,
    val cwd: String? = null,
    val kind: String = "",
    @SerialName("created_at") val createdAt: String? = null,
    @SerialName("tool_name") val toolName: String? = null,
    val questions: List<Question>? = null,
    val plan: String? = null,
    @SerialName("tool_input") val toolInput: JsonElement? = null,
    val summary: String? = null,
    val screen: String? = null,
    val dialog: ScreenDialog? = null,
    val secret: Boolean? = null,
    /** Who asks when it is not Claude Code: "codex", "traex", or an agent runtime's agent ("dsh"; docs/runtimes.md). */
    val agent: String? = null,
    /** What "always allow" allows here; a card from another agent offers it only with this (as on iOS). */
    @SerialName("always_label") val alwaysLabel: String? = null,
) {
    val sessionKey: SessionKey get() = SessionKey(machineId, sessionId)

    /** One line for a notification or a list row. */
    val headline: String
        get() = when (kind) {
            "question" -> questions?.firstOrNull()?.question ?: "Claude 有问题问你"
            "plan" -> "计划等你批准"
            "permission" -> "请求使用 ${toolName ?: "工具"}" + (summary?.takeIf { it.isNotBlank() }?.let { "：$it" } ?: "")
            "screen" -> dialog?.question?.takeIf { it.isNotBlank() } ?: "终端里有对话框等你处理"
            else -> "需要你处理"
        }
}

@Serializable
data class Resolution(
    val answers: Map<String, String>? = null,
    val approve: Boolean? = null,
    val mode: String? = null,
    val feedback: String? = null,
    val allow: Boolean? = null,
    val always: Boolean? = null,
    val message: String? = null,
    val keys: List<String>? = null,
    val text: String? = null,
    val dismiss: Boolean? = null,
    @SerialName("pre_keys") val preKeys: List<String>? = null,
    val option: String? = null,
)

@Serializable
data class Launcher(val name: String, @SerialName("base_url") val baseUrl: String? = null, val model: String? = null)

/**
 * Pinned or muted spaces and agents (`/api/v1/pins`, `/api/v1/mutes`, same shape). Keys are "machine/space name" for
 * spaces (herdr workspace ids change on restart, names don't) and "machine/session id" for agents.
 */
@Serializable
data class Pins(
    val spaces: List<String> = emptyList(),
    val sessions: List<String> = emptyList(),
    /** Mutes only: agents that keep notifications although their space is muted. */
    val allowed: List<String> = emptyList(),
) {
    /** The hub's mute rule: muted itself, or its space is unless the agent was turned back on. */
    fun mutes(session: String, space: String?): Boolean = when {
        session in sessions -> true
        session in allowed -> false
        else -> space != null && space in spaces
    }

    /** Flips one agent's notifications on top of its space's setting (iOS `Pins.togglingMute`). */
    fun togglingMute(session: String, space: String?): Pins {
        val wasMuted = mutes(session, space)
        val spaceMuted = space != null && space in spaces
        val s = sessions - session
        val a = allowed - session
        return when {
            !wasMuted -> copy(sessions = listOf(session) + s, allowed = a)
            spaceMuted -> copy(sessions = s, allowed = listOf(session) + a)
            else -> copy(sessions = s, allowed = a)
        }
    }

    fun toggling(session: String): Pins =
        copy(sessions = if (session in sessions) sessions - session else listOf(session) + sessions)
}

fun Session.pinKey(): String = "$machineId/$sessionId"
fun Session.spacePinKey(): String? = spaceName?.takeIf { it.isNotBlank() }?.let { "$machineId/$it" }
