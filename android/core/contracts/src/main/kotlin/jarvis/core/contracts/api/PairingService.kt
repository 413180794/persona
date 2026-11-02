package jarvis.core.contracts.api

import kotlinx.coroutines.flow.StateFlow

data class PairingRequest(
    val url: String,
    val token: String,
    val hub: String? = null,
    val fp: String? = null,
    val tls: Int? = null,
    val alt: List<String> = emptyList(),
) {
    override fun toString() = "PairingRequest(credentials=redacted)"
}

enum class PairingStage { Parse, Probe, VerifyIdentity, ExchangeCredentials, LoadMachines, Complete }
data class PairingProgress(
    val stage: PairingStage,
    val timestamp: String? = null,
    val candidates: List<ConnectionCandidate> = emptyList(),
)
enum class PairingFailure { InvalidCode, Dns, Refused, Unreachable, Timeout, LocalPermission, Certificate, Expired, Incompatible, TlsRequired, Unknown }
class PairingException(val reason: PairingFailure) : Exception(reason.name)

interface PairingService {
    val isPaired: StateFlow<Boolean>
    suspend fun pair(request: PairingRequest)
    /** Implementations must report actual stages and verify identity before exchanging credentials. */
    suspend fun pair(request: PairingRequest, progress: (PairingProgress) -> Unit) {
        // Legacy adapters cannot safely process pinned pairing metadata.
        if (request.hub != null || request.fp != null || request.tls != null || request.alt.isNotEmpty()) {
            throw PairingException(PairingFailure.Incompatible)
        }
        progress(PairingProgress(PairingStage.ExchangeCredentials))
        pair(request)
    }
    suspend fun unpair()
}
