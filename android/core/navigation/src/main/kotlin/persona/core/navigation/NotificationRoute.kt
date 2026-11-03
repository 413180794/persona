package persona.core.navigation

import android.net.Uri

/** Shared notification/deep-link contract; the app supplies the Navigator implementation. */
data class NotificationRoute(val tab: AppTab?, val chat: ChatRoute? = null) {
    fun uri(): Uri = Uri.Builder().scheme("jarvis").authority("open").apply {
        tab?.let { appendQueryParameter("tab", if (it == AppTab.Jarvis) "jarvis" else "agents") }
        chat?.let {
            appendQueryParameter("machine", it.machineId)
            appendQueryParameter("session", it.sessionId)
        }
    }.build()

    companion object {
        fun conversation(machineId: String, sessionId: String, assistant: Boolean?) = NotificationRoute(
            when (assistant) { true -> AppTab.Jarvis; false -> AppTab.Agents; null -> null }, ChatRoute(machineId, sessionId),
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
            return NotificationRoute(tab, ChatRoute(machine, session))
        }
    }
}
