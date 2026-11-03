package persona.platform.notify

import persona.core.contracts.Interaction
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.InteractionGoneException
import persona.core.designsystem.PendingStatus
import persona.core.domain.answer.RequestExpiry
import java.time.Instant

internal enum class NotificationActionResult { Submitted, Expired, Gone, Locked, Invalid, Duplicate }

/** Claim immediately before sending; a lost response must never cause another resolve. */
internal class NotificationActionExecutor(
    private val center: InteractionCenter,
    private val claim: (String) -> Boolean,
    private val now: () -> Instant = Instant::now,
) {
    suspend fun execute(
        id: String,
        key: String,
        reply: String?,
        locked: () -> Boolean,
        onInteraction: (Interaction) -> Unit = {},
        expiresAt: String? = null,
        onProgress: (PendingStatus) -> Unit = {},
    ): NotificationActionResult {
        if (RequestExpiry.expired(expiresAt, now())) return NotificationActionResult.Expired
        center.pending.value.firstOrNull { it.id == id }?.let { cached ->
            onInteraction(cached)
        }
        onProgress(PendingStatus.Verifying)
        // Always query the hub: a locally pending request may already be handled elsewhere.
        center.refresh()
        val ix = center.pending.value.firstOrNull { it.id == id } ?: return NotificationActionResult.Gone
        onInteraction(ix)
        if (NotificationPolicy.expired(ix, now())) return NotificationActionResult.Expired
        val spec = NotificationPolicy.actions(ix).firstOrNull { it.key == key } ?: return NotificationActionResult.Invalid
        if (spec.authenticationRequired && locked()) return NotificationActionResult.Locked
        val resolution = NotificationPolicy.resolution(ix, key, reply) ?: return NotificationActionResult.Invalid
        if (NotificationPolicy.expired(ix, now())) return NotificationActionResult.Expired
        if (!claim(id)) return NotificationActionResult.Duplicate
        onProgress(PendingStatus.Sending)
        // Persistence and notification updates may cross the deadline; check at the transport boundary.
        if (NotificationPolicy.expired(ix, now())) return NotificationActionResult.Expired
        try { center.resolve(id, resolution) }
        catch (_: InteractionGoneException) { return NotificationActionResult.Gone }
        return NotificationActionResult.Submitted
    }
}
