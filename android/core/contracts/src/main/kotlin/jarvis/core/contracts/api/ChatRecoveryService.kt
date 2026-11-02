package jarvis.core.contracts.api

import jarvis.core.contracts.Session
import jarvis.core.contracts.SessionKey

sealed interface ChatRecoveryResult {
    data class Restored(val session: SessionKey) : ChatRecoveryResult
    data object Unknown : ChatRecoveryResult
    data class Failed(val reason: String) : ChatRecoveryResult
}

/** Keep one instance per pairing. An uncertain resume must only read back session state. */
interface ChatRecoveryService {
    suspend fun resume(session: Session): ChatRecoveryResult
    suspend fun reconnect(machineId: String): ChatActionResult
}
