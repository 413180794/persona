package persona.core.domain.answer

import java.net.URI

/** Link approvals accept only the exact HTTP(S) target that the user opened. */
object ElicitationUrl {
    fun target(value: String?): String? = runCatching {
        val target = value?.trim()?.takeIf(String::isNotEmpty) ?: return null
        val uri = URI(target)
        target.takeIf { uri.scheme?.lowercase() in setOf("http", "https") &&
            !uri.host.isNullOrEmpty() && uri.rawUserInfo == null }
    }.getOrNull()

    fun host(value: String?): String? = target(value)?.let { URI(it).host }
}
