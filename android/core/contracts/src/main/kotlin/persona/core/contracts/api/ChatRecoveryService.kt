package persona.core.contracts.api

import persona.core.contracts.CoreText
import persona.core.contracts.Session
import persona.core.contracts.SessionKey

sealed interface ChatRecoveryResult {
    data class Restored(val session: SessionKey) : ChatRecoveryResult
    data object Unknown : ChatRecoveryResult
    data class Failed(val reason: CoreText) : ChatRecoveryResult
}

/** Keep one instance per pairing. An uncertain resume must only read back session state. */
interface ChatRecoveryService {
    suspend fun resume(session: Session): ChatRecoveryResult
    suspend fun reconnect(machineId: String): ChatActionResult
}
