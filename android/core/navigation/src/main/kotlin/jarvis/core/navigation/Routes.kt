package jarvis.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.Serializable

@Serializable data object AssistantRoute : NavKey
@Serializable data object AgentsRoute : NavKey
@Serializable data object InboxRoute : NavKey
@Serializable data object SettingsRoute : NavKey
@Serializable data object ConnectionSettingsRoute : NavKey
@Serializable enum class ConversationKind { Assistant, Agent }

@Serializable
sealed interface RouteSource {
    @Serializable data class InboxItem(val itemId: String) : RouteSource
    @Serializable data class LarkConversation(val chatId: String) : RouteSource
    @Serializable data class Interaction(val machineId: String, val requestId: String) : RouteSource
}

@Serializable data class ChatRoute(
    val machineId: String,
    val sessionId: String,
    val kind: ConversationKind? = null,
    val source: RouteSource? = null,
) : NavKey

@Serializable data class InboxItemRoute(val itemId: String) : NavKey
@Serializable data class LarkChatRoute(val chatId: String) : NavKey
@Serializable data class InteractionRoute(val chat: ChatRoute, val requestId: String) : NavKey
@Serializable data object PairingRoute : NavKey
@Serializable enum class SettingsPage { Diagnostics, Readiness, Notifications, PhoneData, Appearance, Devices, About, Pairing, Assistant, Hubs }
@Serializable data class SettingsPageRoute(val page: SettingsPage) : NavKey

enum class AppTab { Jarvis, Agents, Inbox, Settings }

interface Navigator {
    fun navigate(key: NavKey)
    fun back()
    fun selectTab(tab: AppTab)
}
