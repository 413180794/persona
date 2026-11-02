package jarvis.core.contracts.api

import jarvis.core.contracts.Session
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/** Bound to one hub and pairing generation; never reuse it after pairing changes. */
interface AgentCreationService {
    val scope: String
    suspend fun launchers(machineId: String): List<AgentLauncher>
    suspend fun directories(machineId: String, path: String = ""): AgentDirectories
    suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult
    suspend fun operation(operationId: String): AgentStartResult
}

@Serializable
data class AgentLauncher(
    val name: String,
    @SerialName("base_url") val baseUrl: String? = null,
    val model: String? = null,
    @SerialName("agent_type") val agentType: String = "claude",
)

@Serializable
data class AgentDirectory(val name: String, val path: String, @SerialName("is_git") val isGit: Boolean = false)

@Serializable
data class AgentRecentDirectory(val path: String, val name: String = "", @SerialName("last_used") val lastUsed: String? = null)

@Serializable
data class AgentDirectories(
    val path: String,
    val parent: String = "",
    val entries: List<AgentDirectory> = emptyList(),
    val recent: List<AgentRecentDirectory> = emptyList(),
)

@Serializable
data class AgentStartRequest(
    @SerialName("machine_id") val machineId: String,
    val cwd: String,
    val launcher: String,
    val prompt: String? = null,
    @SerialName("permission_mode") val permissionMode: String? = null,
    val effort: String? = null,
    /** Opens a new tab in this herdr workspace instead of a new space. */
    @SerialName("workspace_id") val workspaceId: String? = null,
    @SerialName("agent_type") val agentType: String? = null,
    @SerialName("resume_session_id") val resumeSessionId: String? = null,
)

sealed interface AgentStartResult {
    data class Created(val session: Session) : AgentStartResult
    data object Pending : AgentStartResult
    data object Unknown : AgentStartResult
    /** Only an authoritative failed receipt permits editing and a new operation. */
    data class Rejected(val message: String) : AgentStartResult
}
