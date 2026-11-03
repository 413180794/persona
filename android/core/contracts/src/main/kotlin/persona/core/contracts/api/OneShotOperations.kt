package persona.core.contracts.api

import kotlinx.serialization.Serializable

@Serializable
enum class OneShotOperationKind { Resume, Reconnect, Close }

@Serializable
data class OneShotOperation(
    val id: String,
    val kind: OneShotOperationKind,
    val machineId: String,
    val sessionId: String = "",
)

data class OneShotClaim(val operation: OneShotOperation, val firstAttempt: Boolean)

/** Claim must commit before sending. Existing claims permit read-only reconciliation only. */
interface OneShotOperationRepository {
    suspend fun claim(kind: OneShotOperationKind, machineId: String, sessionId: String = ""): OneShotClaim
    /** Forget only after a definite rejection or acknowledgement. */
    suspend fun forget(operation: OneShotOperation)
}
