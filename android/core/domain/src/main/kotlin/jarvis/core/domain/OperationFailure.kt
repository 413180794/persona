package jarvis.core.domain

/** Transport-independent decisions; human-readable server messages never affect recovery. */
enum class FailureCategory { Network, Pairing, Certificate, Incompatible, LocalPermission, Other }
enum class FailureAction { None, Retry, Verify, PairAgain, OpenTerminal, ReviewInput, UpdateHub, GrantPermission }
enum class FailureReason {
    Demo, PairingExpired, CertificateChanged, Incompatible, LocalPermission,
    InputDraft, DialogOpen, StaleSession, Invalid, IdReused, PromptChanged, Expired,
    RateLimited, NotDelivered, Unknown, Delivered, Failed,
}
data class OperationFailure(val reason: FailureReason, val action: FailureAction, val offline: Boolean = false)

fun mapOperationFailure(
    code: String?, delivered: String?, retryable: Boolean,
    category: FailureCategory = FailureCategory.Other,
): OperationFailure {
    fun result(reason: FailureReason, action: FailureAction) = OperationFailure(reason, action)
    if (code == "demo") return result(FailureReason.Demo, FailureAction.None)
    if (code in setOf("pairing_expired", "unauthorized", "device_revoked") || category == FailureCategory.Pairing)
        return result(FailureReason.PairingExpired, FailureAction.PairAgain)
    if (category == FailureCategory.Certificate || code == "certificate_mismatch")
        return result(FailureReason.CertificateChanged, FailureAction.PairAgain)
    if (category == FailureCategory.Incompatible || code in setOf("incompatible", "tls_required"))
        return result(FailureReason.Incompatible, FailureAction.UpdateHub)
    if (category == FailureCategory.LocalPermission)
        return result(FailureReason.LocalPermission, FailureAction.GrantPermission)
    // Delivery evidence takes precedence over retryability and business refusal codes.
    if (delivered == "unknown") return result(FailureReason.Unknown, FailureAction.Verify)
    if (delivered == "yes") return result(FailureReason.Delivered, FailureAction.Verify)
    val business = when (code) {
        "input_draft" -> result(FailureReason.InputDraft, FailureAction.OpenTerminal)
        "dialog_open" -> result(FailureReason.DialogOpen, FailureAction.OpenTerminal)
        "stale_session" -> result(FailureReason.StaleSession, FailureAction.Verify)
        "op_id_reused", "client_id_reused" -> result(FailureReason.IdReused, FailureAction.Verify)
        "invalid", "invalid_request" -> result(FailureReason.Invalid, FailureAction.ReviewInput)
        "prompt_changed" -> result(FailureReason.PromptChanged, FailureAction.Verify)
        "expired", "request_expired", "already_handled" -> result(FailureReason.Expired, FailureAction.OpenTerminal)
        "rate_limited" -> result(FailureReason.RateLimited, if (retryable) FailureAction.Retry else FailureAction.None)
        else -> null
    }
    if (business != null) return business
    if (category == FailureCategory.Network) return OperationFailure(
        if (delivered in setOf("none", "no") || code == "no_network") FailureReason.NotDelivered else FailureReason.Unknown,
        if (delivered in setOf("none", "no") || code == "no_network") {
            if (retryable) FailureAction.Retry else FailureAction.None
        } else FailureAction.Verify,
        offline = true,
    )
    return result(FailureReason.Failed, if (retryable) FailureAction.Retry else FailureAction.None)
}
