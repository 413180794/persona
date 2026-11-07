package persona.android.navigation

import persona.core.contracts.ProtocolCompatibility

import android.net.Uri
import persona.core.navigation.*
import java.net.URI
import java.net.URISyntaxException
import java.net.URLDecoder

/** Notification links name their tab explicitly; links with a `target` are general deep links for [Router]. */
internal fun notificationRoute(uri: Uri): NotificationRoute? =
    if (uri.getQueryParameter("tab") == null || uri.getQueryParameter("target") != null) null
    else NotificationRoute.parse(uri)

/** The tab a notification names wins over session metadata, which may not have loaded on a cold start. */
internal fun Router.open(route: NotificationRoute): ResolvedRoute? {
    val kind = when (route.tab) {
        AppTab.Persona -> ConversationKind.Assistant
        AppTab.Agents -> ConversationKind.Agent
        else -> null
    }
    val chat = route.chat
    val tab = route.tab
    return when {
        chat != null -> chat.copy(kind = kind ?: chat.kind).let { target ->
            parse(RouteInput.Internal(route.request?.let { InteractionRoute(target, it) } ?: target))
        }
        tab != null -> parse(RouteInput.Internal(tabRoot(tab)))?.let { root ->
            route.card?.takeIf { tab == AppTab.Agents }?.let { ResolvedRoute(root.tab, root.key, card = it) } ?: root
        }
        else -> null
    }
}

/** The notification module's terminal fallback (`target=terminal`); the shell keeps the conversation below it. */
internal fun terminalLink(link: String): ResolvedRoute? {
    val uri = try { URI(link) } catch (_: URISyntaxException) { return null }
    if (uri.scheme !in ProtocolCompatibility.appSchemes || uri.rawAuthority != "open" || !uri.rawPath.isNullOrEmpty() || uri.rawFragment != null) return null
    val parameters = mutableMapOf<String, String>()
    try {
        for (part in uri.rawQuery.orEmpty().split('&').filter { it.isNotEmpty() }) {
            if (parameters.put(decode(part.substringBefore('=')), decode(part.substringAfter('=', ""))) != null) return null
        }
    } catch (_: IllegalArgumentException) { return null }
    if (parameters["target"] != "terminal") return null
    val machine = parameters["machine"]?.takeIf(String::isNotBlank) ?: return null
    val session = parameters["session"]?.takeIf(String::isNotBlank) ?: return null
    return ResolvedRoute(AppTab.Agents, TerminalRoute(machine, session))
}

private fun decode(value: String): String = URLDecoder.decode(value.replace("+", "%2B"), "UTF-8")

/** Restore a resolved destination; a terminal also restores its conversation so back returns there. */
internal fun ResolvedRoute.open(stacks: Map<AppTab, MutableList<androidx.navigation3.runtime.NavKey>>): AppTab {
    val terminal = key as? TerminalRoute ?: return restore(stacks)
    ResolvedRoute(tab, ChatRoute(terminal.machineId, terminal.sessionId, ConversationKind.Agent)).restore(stacks)
    val stack = stacks.getValue(tab)
    if (stack.last() != terminal) stack.add(terminal)
    return tab
}
