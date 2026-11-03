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
    suspend fun config(): AssistantConfig = throw UnsupportedOperationException("助手设置尚未接入")
    suspend fun updateConfig(update: AssistantConfigUpdate): AssistantConfig = throw UnsupportedOperationException("助手设置尚未接入")
    /** The launchers a machine offers (GET /api/v1/machines/{id}/launchers). */
    suspend fun launchers(machineId: String): List<AgentLauncher> = throw UnsupportedOperationException("助手设置尚未接入")
    suspend fun endConversation(session: SessionKey)
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
