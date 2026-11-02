@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package jarvis.core.contracts

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.KeepGeneratedSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import java.time.Instant
import java.time.OffsetDateTime

// Wire types mirror docs/protocol.md and tolerate optional enhancements according to D5.

val JsonCodec = Json {
    ignoreUnknownKeys = true
    explicitNulls = false
    coerceInputValues = true
    encodeDefaults = false
}

/**
 * RFC3339 from the hub; D5 treats dates before 2000, including Go zero time, as unset. A hub running in a local zone
 * sends offsets (`+08:00`), and [Instant.parse] on Android 12 and 13 accepts only `Z`, so offsets go through [OffsetDateTime].
 */
fun parseTime(s: String?): Instant? {
    if (s.isNullOrEmpty()) return null
    val instant = runCatching { Instant.parse(s) }.recoverCatching { OffsetDateTime.parse(s).toInstant() }.getOrNull()
    return instant?.takeUnless { it.isBefore(Instant.parse("2000-01-01T00:00:00Z")) }
}

@KeepGeneratedSerializer
@Serializable(with = MachineSerializer::class)
data class Machine(
    @SerialName("id") val id: String,
    @SerialName("name") val name: String = "",
    @SerialName("kind") val kind: String = "",
    @SerialName("icon") val icon: String? = null,
    @SerialName("ssh_host") val sshHost: String? = null,
    @SerialName("online") val online: Boolean = false,
    @SerialName("error") val error: String? = null,
    @SerialName("hostname") val hostname: String? = null,
    @SerialName("os") val os: String? = null,
    @SerialName("herdr_version") val herdrVersion: String? = null,
    @SerialName("claude_version") val claudeVersion: String? = null,
    @WireTime @SerialName("last_seen") val lastSeen: String? = null,
    val arch: String? = null,
    val home: String? = null,
    @SerialName("node_version") val nodeVersion: String? = null,
    val system: MachineSystem? = null,
    @SerialName("node_mismatch") val nodeMismatch: Boolean? = null,
) {
    val displayName: String get() = name.ifEmpty { id }
}

/** Unified agent state computed by the hub (docs/agent-states.md). */
enum class AgentState(val wire: String, val priority: Int) {
    NeedsYou("needs_you", 0), Unread("unread", 1), Working("working", 2), Idle("idle", 3), Unknown("unknown", 4),
    Offline("offline", 5);

    companion object {
        fun of(wire: String?, status: String?): AgentState =
            if (wire != null) entries.firstOrNull { it.wire == wire } ?: Unknown else when (status) {
                "blocked" -> NeedsYou
                "working" -> Working
                "done" -> Unread
                "idle" -> Idle
                "offline" -> Offline
                else -> Unknown
            }
    }
}

@KeepGeneratedSerializer
@Serializable(with = LoopInfoSerializer::class)
data class LoopInfo(
    @SerialName("kind") val kind: String = "",
    @SerialName("prompt") val prompt: String? = null,
    @SerialName("interval_seconds") val intervalSeconds: Int? = null,
    @WireTime @SerialName("next_at") val nextAt: String? = null,
    @SerialName("cron") val cron: String? = null,
    val reason: String? = null,
    val ticks: Int? = null,
)

@KeepGeneratedSerializer
@Serializable(with = BackgroundTaskSerializer::class)
data class BackgroundTask(
    @SerialName("id") val id: String = "",
    @SerialName("kind") val kind: String = "",
    @SerialName("description") val description: String? = null,
    @SerialName("command") val command: String? = null,
    @SerialName("output_file") val outputFile: String? = null,
    @WireTime @SerialName("started_at") val startedAt: String? = null,
)

@KeepGeneratedSerializer
@Serializable(with = SessionSerializer::class)
data class Session(
    @SerialName("machine_id") val machineId: String = "",
    @SerialName("session_id") val sessionId: String,
    @SerialName("title") val title: String = "",
    @SerialName("cwd") val cwd: String = "",
    @WireEnum("idle", "working", "blocked", "done", "offline", "unknown")
    @SerialName("status") val status: String = "unknown",
    @SerialName("tab_id") val tabId: String? = null,
    @SerialName("workspace_id") val workspaceId: String? = null,
    @SerialName("agent_name") val agentName: String? = null,
    @SerialName("started_by_jarvis") val startedByJarvis: Boolean = false,
    val effort: String? = null,
    @SerialName("space_kind") val spaceKind: String? = null,
    @SerialName("space_index") val spaceIndex: Int? = null,
    @SerialName("slot_id") val slotId: String? = null,
    @WireTime @SerialName("state_since") val stateSince: String? = null,
    val subagents: List<Subagent>? = null,
    @SerialName("pane_id") val paneId: String? = null,
    @SerialName("agent") val agent: String? = null,
    @SerialName("launcher") val launcher: String? = null,
    @SerialName("model") val model: String? = null,
    @SerialName("permission_mode") val permissionMode: String? = null,
    @SerialName("assistant") val assistant: Boolean = false,
    @SerialName("space_id") val spaceId: String? = null,
    @SerialName("space_name") val spaceName: String? = null,
    @SerialName("slot_name") val slotName: String? = null,
    @SerialName("slot_index") val slotIndex: Int? = null,
    @WireEnum("needs_you", "unread", "working", "idle", "offline", "unknown")
    @SerialName("state") val state: String? = AgentState.of(null, status).wire,
    @WireTime @SerialName("seen_at") val seenAt: String? = null,
    @SerialName("queued") val queued: List<String>? = null,
    @SerialName("background") val background: List<BackgroundTask>? = null,
    @SerialName("loop") val loop: LoopInfo? = null,
    @WireTime @SerialName("updated_at") val updatedAt: String? = null,
    @SerialName("last_text") val lastText: String? = null,
    @SerialName("pending_interactions") val pendingInteractions: Int = 0,
    @SerialName("rev") val rev: Long? = null,
    @SerialName("suggestion") val suggestion: String? = null,
) {
    val key: SessionKey get() = SessionKey(machineId, sessionId)
    val agentState: AgentState get() = AgentState.of(state, status)
    val folderName: String get() = cwd.trimEnd('/').substringAfterLast('/')
    val displayTitle: String get() = if (title.isEmpty() || title == "Claude Code") folderName.ifEmpty { sessionId.take(8) } else title
    val agentLabel: String get() = when (agent) { "codex" -> "Codex"; "traex" -> "TRAEX"; else -> "Claude" }
    val isLive: Boolean get() = status != "offline"
    val updated: Instant? get() = parseTime(updatedAt)
}

data class SessionKey(val machineId: String, val sessionId: String) {
    val path: String get() = "$machineId/$sessionId"
}

@KeepGeneratedSerializer
@Serializable(with = ImagePayloadSerializer::class)
data class ImagePayload(
    @SerialName("mime") val mime: String = "image/png",
    @SerialName("data") val data: String = "",
    @SerialName("lazy") val lazy: Boolean? = null,
    @SerialName("width") val width: Int? = null,
    @SerialName("height") val height: Int? = null,
    val size: Int? = null,
)

@KeepGeneratedSerializer
@Serializable(with = ItemSerializer::class)
data class Item(
    @SerialName("id") val id: String,
    @SerialName("seq") val seq: Int = 0,
    @WireTime @SerialName("ts") val ts: String? = null,
    @SerialName("role") val role: String = "",
    @WireEnum("text", "thinking", "tool_use", "tool_result", "notice", "recap", "image", "fold", "turn_duration", "unknown")
    @SerialName("kind") val kind: String = "unknown",
    @SerialName("text") val text: String? = null,
    @SerialName("tool_use_id") val toolUseId: String? = null,
    @SerialName("tool_name") val toolName: String? = null,
    @SerialName("tool_input") val toolInput: JsonElement? = null,
    @SerialName("is_error") val isError: Boolean? = null,
    @SerialName("full_size") val fullSize: Int? = null,
    @SerialName("image") val image: ImagePayload? = null,
    @SerialName("queued") val queued: Boolean? = null,
    @SerialName("from") val from: String? = null,
    @SerialName("wake") val wake: Boolean? = null,
    val task: TaskNote? = null,
    val fold: Fold? = null,
    @SerialName("turn_duration") val turnDuration: TurnDuration? = null,
)

@KeepGeneratedSerializer
@Serializable(with = TurnDurationSerializer::class)
data class TurnDuration(
    @SerialName("duration_ms") val durationMs: Long? = null,
    val source: String = "",
    @SerialName("turn_id") val turnId: String? = null,
    @SerialName("parent_uuid") val parentUuid: String? = null,
    @SerialName("user_item_id") val userItemId: String? = null,
)

object TurnDurationSerializer : WireSerializer<TurnDuration>(TurnDuration.generatedSerializer())

@KeepGeneratedSerializer
@Serializable(with = QuestionOptionSerializer::class)
data class QuestionOption(@SerialName("label") val label: String = "", @SerialName("description") val description: String? = null)

@KeepGeneratedSerializer
@Serializable(with = QuestionSerializer::class)
data class Question(
    @SerialName("question") val question: String = "",
    @SerialName("header") val header: String? = null,
    @SerialName("multi_select") val multiSelect: Boolean = false,
    @SerialName("options") val options: List<QuestionOption> = emptyList(),
)

@KeepGeneratedSerializer
@Serializable(with = DialogOptionSerializer::class)
data class DialogOption(@SerialName("key") val key: String = "", @SerialName("label") val label: String = "", @SerialName("detail") val detail: String? = null, @SerialName("kind") val kind: String? = null, val keys: List<String>? = null)

/** A terminal dialog the hub parsed (server/internal/screen); answer with Resolution.option. */
@KeepGeneratedSerializer
@Serializable(with = ScreenDialogSerializer::class)
data class ScreenDialog(
    @SerialName("header") val header: String? = null,
    @SerialName("question") val question: String = "",
    @SerialName("detail") val detail: String? = null,
    @SerialName("options") val options: List<DialogOption> = emptyList(),
    @SerialName("rev") val rev: String = "",
)

@KeepGeneratedSerializer
@Serializable(with = InteractionSerializer::class)
data class Interaction(
    @SerialName("id") val id: String,
    @SerialName("machine_id") val machineId: String = "",
    @SerialName("session_id") val sessionId: String = "",
    @SerialName("session_title") val sessionTitle: String? = null,
    @SerialName("cwd") val cwd: String? = null,
    @WireEnum("question", "plan", "permission", "screen", "elicitation", "unknown")
    @SerialName("kind") val kind: String = "unknown",
    @WireTime @SerialName("expires_at") val expiresAt: String? = null,
    @SerialName("tool_use_id") val toolUseId: String? = null,
    val resumable: Boolean? = null,
    @WireTime @SerialName("created_at") val createdAt: String? = null,
    @SerialName("tool_name") val toolName: String? = null,
    @SerialName("questions") val questions: List<Question>? = null,
    @SerialName("plan") val plan: String? = null,
    @SerialName("tool_input") val toolInput: JsonElement? = null,
    @SerialName("summary") val summary: String? = null,
    @SerialName("screen") val screen: String? = null,
    @SerialName("dialog") val dialog: ScreenDialog? = null,
    @SerialName("secret") val secret: Boolean? = null,
    @SerialName("agent_id") val agentId: String? = null,
    @SerialName("agent_type") val agentType: String? = null,
    @SerialName("mcp_server_name") val mcpServerName: String? = null,
    @SerialName("mcp_config_source") val mcpConfigSource: String? = null,
    val elicitation: Elicitation? = null,
) {
    val sessionKey: SessionKey get() = SessionKey(machineId, sessionId)

    /** One line for a notification or a list row. */
    val headline: String
        get() = when (kind) {
            "question" -> questions?.firstOrNull()?.question ?: "Claude 有问题问你"
            "elicitation" -> "MCP 表单等你填写"
            "plan" -> "计划等你批准"
            "permission" -> "请求使用 ${toolName ?: "工具"}" + (summary?.takeIf { it.isNotBlank() }?.let { "：$it" } ?: "")
            "screen" -> dialog?.question?.takeIf { it.isNotBlank() } ?: "终端里有对话框等你处理"
            else -> "需要你处理"
        }
}

@Serializable
data class Resolution(
    @SerialName("answers") val answers: Map<String, String>? = null,
    @SerialName("approve") val approve: Boolean? = null,
    @SerialName("mode") val mode: String? = null,
    @SerialName("feedback") val feedback: String? = null,
    @SerialName("allow") val allow: Boolean? = null,
    @SerialName("always") val always: Boolean? = null,
    @SerialName("message") val message: String? = null,
    @SerialName("keys") val keys: List<String>? = null,
    @SerialName("text") val text: String? = null,
    @SerialName("dismiss") val dismiss: Boolean? = null,
    @SerialName("pre_keys") val preKeys: List<String>? = null,
    @SerialName("option") val option: String? = null,
    val action: String? = null,
    val content: kotlinx.serialization.json.JsonObject? = null,
)

@Serializable
data class Launcher(@SerialName("name") val name: String, @SerialName("base_url") val baseUrl: String? = null, @SerialName("model") val model: String? = null)

object MachineSerializer : WireSerializer<Machine>(Machine.generatedSerializer())
object LoopInfoSerializer : WireSerializer<LoopInfo>(LoopInfo.generatedSerializer())
object BackgroundTaskSerializer : WireSerializer<BackgroundTask>(BackgroundTask.generatedSerializer())
object SessionSerializer : WireSerializer<Session>(Session.generatedSerializer())
object ImagePayloadSerializer : WireSerializer<ImagePayload>(ImagePayload.generatedSerializer())
object ItemSerializer : WireSerializer<Item>(Item.generatedSerializer())
object QuestionOptionSerializer : WireSerializer<QuestionOption>(QuestionOption.generatedSerializer())
object QuestionSerializer : WireSerializer<Question>(Question.generatedSerializer())
object DialogOptionSerializer : WireSerializer<DialogOption>(DialogOption.generatedSerializer())
object ScreenDialogSerializer : WireSerializer<ScreenDialog>(ScreenDialog.generatedSerializer())
object InteractionSerializer : WireSerializer<Interaction>(Interaction.generatedSerializer())
