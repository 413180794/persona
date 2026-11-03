package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import java.net.URI
import java.net.URLDecoder

sealed interface RouteInput {
    data class DeepLink(val url: String) : RouteInput
    data class Notification(
        val sessionPath: String,
        val kind: ConversationKind? = null,
        val requestId: String? = null,
    ) : RouteInput
    data class Share(val chat: ChatRoute) : RouteInput
    data class Internal(val key: NavKey) : RouteInput
}

/** Pairing credentials are transient input, never a serialized back-stack key. */
class ResolvedRoute(val tab: AppTab, val key: NavKey, val pairingLink: String? = null)

class Router(
    private val conversationKind: (machineId: String, sessionId: String) -> ConversationKind? = { _, _ -> null },
) {
    /** Null means invalid input or missing session metadata; retain pending legacy input until metadata arrives. */
    fun parse(input: RouteInput): ResolvedRoute? = when (input) {
        is RouteInput.DeepLink -> deepLink(input.url)
        is RouteInput.Notification -> sessionPath(input.sessionPath, input.kind)?.let { chat ->
            resolve(input.requestId?.let { InteractionRoute(chat, it) } ?: chat)
        }
        is RouteInput.Share -> resolve(input.chat)
        is RouteInput.Internal -> resolve(input.key)
    }

    private fun resolve(key: NavKey): ResolvedRoute? = when (key) {
        AssistantRoute -> ResolvedRoute(AppTab.Jarvis, key)
        AgentsRoute -> ResolvedRoute(AppTab.Agents, key)
        InboxRoute -> ResolvedRoute(AppTab.Inbox, key)
        SettingsRoute -> ResolvedRoute(AppTab.Settings, key)
        ConnectionSettingsRoute -> resolve(SettingsPageRoute(SettingsPage.Diagnostics))
        PairingRoute, is SettingsPageRoute -> ResolvedRoute(AppTab.Settings, key)
        is InboxItemRoute -> key.takeIf { it.itemId.isNotBlank() }?.let { ResolvedRoute(AppTab.Inbox, it) }
        is LarkChatRoute -> key.takeIf { it.chatId.isNotBlank() }?.let { ResolvedRoute(AppTab.Inbox, it) }
        is ChatRoute -> normalizedChat(key)?.let { ResolvedRoute(it.kind.tab, it) }
        is InteractionRoute -> normalizedChat(key.chat)?.takeIf { key.requestId.isNotBlank() }?.let {
            ResolvedRoute(it.kind.tab, key.copy(chat = it))
        }
        else -> null
    }

    private fun normalizedChat(chat: ChatRoute): ChatRoute? {
        if (chat.machineId.isBlank() || chat.sessionId.isBlank()) return null
        val kind = chat.kind ?: conversationKind(chat.machineId, chat.sessionId) ?: return null
        val validSource = when (val source = chat.source) {
            null -> true
            is RouteSource.InboxItem -> source.itemId.isNotBlank()
            is RouteSource.LarkConversation -> source.chatId.isNotBlank()
            is RouteSource.Interaction -> source.machineId.isNotBlank() && source.requestId.isNotBlank()
        }
        return chat.copy(kind = kind).takeIf { validSource }
    }

    private fun deepLink(url: String): ResolvedRoute? {
        val uri = try { URI(url) } catch (_: IllegalArgumentException) { return null }
            catch (_: java.net.URISyntaxException) { return null }
        if (uri.scheme != "jarvis" || uri.rawAuthority !in setOf("open", "pair") ||
            !uri.rawPath.isNullOrEmpty() || uri.rawFragment != null) return null
        if (uri.host == "pair") {
            // PairingInput remains responsible for validating credentials and transport policy.
            return ResolvedRoute(AppTab.Settings, PairingRoute, url)
        }
        val parameters = linkedMapOf<String, String>()
        try {
            for (part in uri.rawQuery.orEmpty().split('&').filter { it.isNotEmpty() }) {
                val name = decode(part.substringBefore('='))
                val value = decode(part.substringAfter('=', ""))
                if (parameters.put(name, value) != null) return null
            }
        } catch (_: IllegalArgumentException) { return null }
        val kind = when (parameters["kind"]) {
            null -> null
            "assistant" -> ConversationKind.Assistant
            "agent" -> ConversationKind.Agent
            else -> return null
        }
        val key: NavKey = when (parameters["target"] ?: "chat") {
            "chat", "request" -> {
                val chat = ChatRoute(parameters["machine"] ?: return null, parameters["session"] ?: return null, kind)
                if (parameters["target"] == "request") InteractionRoute(chat, parameters["request"] ?: return null)
                else chat
            }
            "inbox" -> InboxItemRoute(parameters["item"] ?: return null)
            "lark" -> LarkChatRoute(parameters["chat"] ?: return null)
            "settings" -> SettingsPageRoute(SettingsPage.entries.firstOrNull {
                it.name.equals(parameters["page"], ignoreCase = true)
            } ?: return null)
            "tab" -> tabRoot(AppTab.entries.firstOrNull { it.name.equals(parameters["tab"], ignoreCase = true) } ?: return null)
            else -> return null
        }
        return resolve(key)
    }
}

private fun decode(value: String): String = URLDecoder.decode(value.replace("+", "%2B"), "UTF-8")

internal fun sessionPath(path: String, kind: ConversationKind?): ChatRoute? {
    val separator = path.indexOf('/')
    if (separator <= 0 || separator == path.lastIndex) return null
    return ChatRoute(path.substring(0, separator), path.substring(separator + 1), kind)
}

private val ConversationKind?.tab: AppTab
    get() = if (this == ConversationKind.Assistant) AppTab.Jarvis else AppTab.Agents

fun tabRoot(tab: AppTab): NavKey = when (tab) {
    AppTab.Jarvis -> AssistantRoute
    AppTab.Agents -> AgentsRoute
    AppTab.Inbox -> InboxRoute
    AppTab.Settings -> SettingsRoute
}

/** Select the returned tab after applying this to the app-owned stacks. */
fun ResolvedRoute.restore(stacks: Map<AppTab, MutableList<NavKey>>): AppTab {
    val stack = stacks.getValue(tab)
    if (stack.isEmpty()) stack.add(tabRoot(tab))
    if (key == tabRoot(tab)) return tab
    // Move legacy entries out of the wrong tab, then pop to an existing target.
    stacks.filterKeys { it != tab }.values.forEach { other -> other.removeAll { sameTarget(it, key) } }
    val existing = stack.indexOfFirst { sameTarget(it, key) }
    if (existing < 0) stack.add(key)
    else {
        while (stack.lastIndex > existing) stack.removeAt(stack.lastIndex)
        // Preserve the existing NavKey and its UI state when only provenance changed.
        val previous = stack[existing]
        if (previous == ConnectionSettingsRoute ||
            previous is ChatRoute && key is ChatRoute && previous.kind != key.kind ||
            previous is InteractionRoute && key is InteractionRoute && previous.chat.kind != key.chat.kind) {
            stack[existing] = key
        }
    }
    return tab
}

private fun sameTarget(left: NavKey, right: NavKey): Boolean = when {
    left is ChatRoute && right is ChatRoute -> left.machineId == right.machineId && left.sessionId == right.sessionId
    left is InteractionRoute && right is InteractionRoute -> left.requestId == right.requestId && sameTarget(left.chat, right.chat)
    left == ConnectionSettingsRoute -> right == SettingsPageRoute(SettingsPage.Diagnostics)
    else -> left == right
}
