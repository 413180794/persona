package persona.core.domain.answer

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import java.time.Instant
import java.time.Duration

/** Missing deadlines support legacy requests; malformed deadlines never authorize submission. */
object RequestExpiry {
    val expiredLabel = CoreText.of(Key.RequestExpired)
    val terminalLabel = CoreText.of(Key.OpenTerminal)

    fun expired(expiresAt: String?, now: Instant): Boolean = expiresAt?.let {
        runCatching { !Instant.parse(it).isAfter(now) }.getOrDefault(true)
    } ?: false

    fun label(expiresAt: String?, now: Instant): CoreText? {
        expiresAt ?: return null
        if (expired(expiresAt, now)) return expiredLabel
        val remaining = Duration.between(now, Instant.parse(expiresAt))
        return when {
            remaining.seconds < 60 -> CoreText.of(Key.ExpiresSoon)
            remaining.toMinutes() < 60 -> CoreText.of(Key.ExpiresInMinutes, remaining.toMinutes().toInt())
            remaining.toHours() < 24 -> CoreText.of(Key.ExpiresInHours, remaining.toHours().toInt())
            else -> CoreText.of(Key.ExpiresInDays, remaining.toDays().toInt())
        }
    }
}
