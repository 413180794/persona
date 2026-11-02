package jarvis.core.domain.answer

import java.time.Instant
import java.time.Duration

/** Missing deadlines support legacy requests; malformed deadlines never authorize submission. */
object RequestExpiry {
    const val expiredLabel = "已过期，回到终端处理"
    const val terminalLabel = "打开终端"

    fun expired(expiresAt: String?, now: Instant): Boolean = expiresAt?.let {
        runCatching { !Instant.parse(it).isAfter(now) }.getOrDefault(true)
    } ?: false

    fun label(expiresAt: String?, now: Instant): String? {
        expiresAt ?: return null
        if (expired(expiresAt, now)) return expiredLabel
        val remaining = Duration.between(now, Instant.parse(expiresAt))
        return when {
            remaining.seconds < 60 -> "即将过期"
            remaining.toMinutes() < 60 -> "${remaining.toMinutes()} 分钟后过期"
            remaining.toHours() < 24 -> "${remaining.toHours()} 小时后过期"
            else -> "${remaining.toDays()} 天后过期"
        }
    }
}
