package persona.core.contracts.api

import persona.core.contracts.Session
import persona.core.contracts.SessionKey

/** Recoverable Agents archives; never deletes the computer's transcript or archives Persona conversations. */
interface AgentArchives {
    val supported: Boolean
    /** In a merged list, capability belongs to the session's own hub. */
    fun supports(key: SessionKey): Boolean = supported
    suspend fun list(): List<Session>
    suspend fun archive(key: SessionKey)
    suspend fun restore(key: SessionKey)

    companion object {
        const val FEATURE = "agent_session_archives"
    }
}
