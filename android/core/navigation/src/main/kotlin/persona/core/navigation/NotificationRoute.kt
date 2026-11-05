package persona.core.navigation

import android.net.Uri

/** Shared notification/deep-link contract; the app supplies the Navigator implementation. */
/** [request]: the interaction the notification is about, shown first in its conversation's request panel. */
data class NotificationRoute(val tab: AppTab?, val chat: ChatRoute? = null, val request: String? = null) {
    fun uri(): Uri = Uri.Builder().scheme("jarvis").authority("open").apply {
        tab?.let { appendQueryParameter("tab", if (it == AppTab.Jarvis) "jarvis" else "agents") }
        chat?.let {
            appendQueryParameter("machine", it.machineId)
            appendQueryParameter("session", it.sessionId)
            request?.let { id -> appendQueryParameter("request", id) }
        }
    }.build()

    companion object {
        fun conversation(machineId: String, sessionId: String, assistant: Boolean?, request: String? = null) = NotificationRoute(
            when (assistant) { true -> AppTab.Jarvis; false -> AppTab.Agents; null -> null }, ChatRoute(machineId, sessionId),
            request?.takeIf { it.isNotBlank() },
        )

        fun parse(uri: Uri): NotificationRoute? {
            if (uri.scheme !in setOf("persona", "jarvis") || uri.host != "open") return null
            val tab = when (uri.getQueryParameter("tab")) {
                "jarvis" -> AppTab.Jarvis
                "agents" -> AppTab.Agents
                null -> null
                else -> return null
            }
            val machine = uri.getQueryParameter("machine")
            val session = uri.getQueryParameter("session")
            if (machine == null && session == null) return NotificationRoute(tab)
            if (machine.isNullOrBlank() || session.isNullOrBlank()) return null
            return NotificationRoute(tab, ChatRoute(machine, session), uri.getQueryParameter("request")?.takeIf { it.isNotBlank() })
        }
    }
}
