package persona.core.contracts.api

import persona.core.contracts.SessionKey
import kotlinx.serialization.EncodeDefault
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Pairing-scoped assistant actions; the composition root supplies the active transport. */
interface AssistantManagement {
    suspend fun warm()
    // Where the assistant runs (设置 › 助手). Defaults keep transports and fakes written before the page compiling.
    suspend fun config(): AssistantConfig = throw UnsupportedOperationException("Assistant settings unavailable")
    suspend fun updateConfig(update: AssistantConfigUpdate): AssistantConfig = throw UnsupportedOperationException("Assistant settings unavailable")
    /** The launchers a machine offers (GET /api/v1/machines/{id}/launchers). */
    suspend fun launchers(machineId: String): List<AgentLauncher> = throw UnsupportedOperationException("Assistant settings unavailable")
    suspend fun endConversation(session: SessionKey)
    /**
     * Compacts an assistant conversation's context now (POST /api/v1/assistant/conversations/{id}/compact, hub feature
     * `assistant_compact`); its progress comes back on the session's `compaction`. A hub that refuses (409: `busy`,
     * `compacting`, `gone`, `unsupported`) throws [AssistantRefused] with its own words.
     */
    suspend fun compactConversation(session: SessionKey): Unit = throw UnsupportedOperationException("Compact context unavailable")
    /**
     * Puts the main conversation away for a new one (POST /api/v1/assistant/main/new, hub feature `assistant_main_new`):
     * it is renamed and archived, and the next message to the main conversation starts a new one. [carrySummary] brings
     * what the old one left; [expect] is the main conversation on screen (a hub where another device replaced it since
     * refuses with `main_changed`, as [AssistantRefused]; `busy` while it answers or compacts).
     */
    suspend fun newMainConversation(carrySummary: Boolean, expect: String?): NewMainResult =
        throw UnsupportedOperationException("New main chat unavailable")
    suspend fun routines(): List<Routine>
    suspend fun setRoutines(list: List<Routine>): List<Routine>
    suspend fun runRoutine(id: String): SessionKey
    suspend fun followups(): List<Followup>
    suspend fun cancelFollowup(id: String)
    suspend fun runFollowup(id: String): SessionKey
    suspend fun runs(): List<Run>
    suspend fun notes(): AssistantNotes
    suspend fun setNotes(text: String)
    suspend fun memory(): List<MemoryFile>
    suspend fun saveMemory(name: String, content: String)
    suspend fun deleteMemory(name: String)
}

@OptIn(ExperimentalSerializationApi::class)
@Serializable
data class Routine(
    // Everything the app edits is always on the wire: JsonCodec leaves out values equal to their default, and
    // the hub reads what's missing as empty or false — a 09:00 routine lost its time (400 "needs a time"), an
    // enabled one would turn off. The hub's own fields below stay optional.
    @EncodeDefault val id: String = "",
    @EncodeDefault val title: String = "",
    @EncodeDefault val prompt: String = "",
    @EncodeDefault val time: String = "09:00",
    /** 1 = Monday … 7 = Sunday; empty = every day. */
    @EncodeDefault val weekdays: List<Int> = emptyList(),
    @EncodeDefault val enabled: Boolean = true,
    @SerialName("created_by") val createdBy: String? = null,
    @SerialName("last_run") val lastRun: String? = null,
    @SerialName("last_session_id") val lastSessionId: String? = null,
    @SerialName("last_status") val lastStatus: String? = null,
    @SerialName("last_error") val lastError: String? = null,
    @SerialName("next_run") val nextRun: String? = null,
)

@Serializable
data class Followup(
    val id: String = "",
    val at: String? = null,
    val title: String? = null,
    val prompt: String = "",
    @SerialName("session_id") val sessionId: String? = null,
    @SerialName("machine_id") val machineId: String? = null,
)

@Serializable
data class Run(
    val id: String = "",
    val kind: String = "",
    val title: String = "",
    val at: String? = null,
    val status: String = "",
    val error: String? = null,
    val manual: Boolean = false,
    @SerialName("session_id") val sessionId: String? = null,
    @SerialName("machine_id") val machineId: String? = null,
    /** The routine's or follow-up's id; one routine's history is the runs that carry it. */
    @SerialName("ref_id") val refId: String = "",
    /** When it was meant to run; [at] after this is how late it ran. */
    val due: String? = null,
)

@Serializable
data class AssistantNotes(
    val notes: String? = null,
    @SerialName("notes_effective") val effective: String = "",
    @SerialName("machine_id") val machineId: String = "",
)

@Serializable
data class MemoryFile(val name: String = "", val content: String = "", @SerialName("updated_at") val updatedAt: String? = null)

/** What 新建主对话 did: the conversation put away ([archived], empty when there was none) and whether any of it carries over. */
data class NewMainResult(val archived: String = "", val carried: Boolean = false)

/** The hub refused an assistant action (409 with a `code`); [message] is the hub's own words, fit to show as they are. */
class AssistantRefused(val code: String, message: String) : Exception(message)
