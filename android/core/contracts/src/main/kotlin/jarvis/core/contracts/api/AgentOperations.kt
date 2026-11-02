package jarvis.core.contracts.api

import jarvis.core.contracts.SessionKey

/** Hub preferences use machine/space-name keys, while renaming uses workspace IDs. */
data class AgentOperationPreferences(
    val spaces: List<String> = emptyList(),
    val sessions: List<String> = emptyList(),
    val allowed: Set<String> = emptySet(),
)

interface AgentOperations {
    val scope: String
    suspend fun pins(): AgentOperationPreferences
    suspend fun mutes(): AgentOperationPreferences
    suspend fun savePins(value: AgentOperationPreferences)
    suspend fun saveMutes(value: AgentOperationPreferences)
    suspend fun renameSession(key: SessionKey, title: String?, tabName: String?)
    suspend fun renameSpace(machineId: String, workspaceId: String, name: String)
    suspend fun closeSession(key: SessionKey)
}

/** An ambiguous write must be verified before repeating a one-shot action. */
class AgentOperationFailure(val uncertain: Boolean, cause: Throwable) : Exception(cause)
