package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable @SerialName("jarvis.core.navigation.AssistantRoute") data object AssistantRoute : NavKey
@Serializable @SerialName("jarvis.core.navigation.AgentsRoute") data object AgentsRoute : NavKey
@Serializable @SerialName("jarvis.core.navigation.InboxRoute") data object InboxRoute : NavKey
@Serializable @SerialName("jarvis.core.navigation.SettingsRoute") data object SettingsRoute : NavKey
@Serializable @SerialName("jarvis.core.navigation.ConnectionSettingsRoute") data object ConnectionSettingsRoute : NavKey
@Serializable enum class ConversationKind { Assistant, Agent }

@Serializable
@SerialName("jarvis.core.navigation.RouteSource")
sealed interface RouteSource {
    @Serializable @SerialName("jarvis.core.navigation.RouteSource.InboxItem") data class InboxItem(val itemId: String) : RouteSource
    @Serializable @SerialName("jarvis.core.navigation.RouteSource.LarkConversation") data class LarkConversation(val chatId: String) : RouteSource
    @Serializable @SerialName("jarvis.core.navigation.RouteSource.Interaction") data class Interaction(val machineId: String, val requestId: String) : RouteSource
}

@Serializable @SerialName("jarvis.core.navigation.ChatRoute") data class ChatRoute(
    val machineId: String,
    val sessionId: String,
    val kind: ConversationKind? = null,
    val source: RouteSource? = null,
) : NavKey

@Serializable @SerialName("jarvis.core.navigation.InboxItemRoute") data class InboxItemRoute(val itemId: String) : NavKey
@Serializable @SerialName("jarvis.core.navigation.LarkChatRoute") data class LarkChatRoute(val chatId: String) : NavKey
@Serializable @SerialName("jarvis.core.navigation.InteractionRoute") data class InteractionRoute(val chat: ChatRoute, val requestId: String) : NavKey
@Serializable @SerialName("jarvis.core.navigation.PairingRoute") data object PairingRoute : NavKey
@Serializable @SerialName("jarvis.core.navigation.TodosRoute") data object TodosRoute : NavKey
@Serializable @SerialName("jarvis.core.navigation.TodoDetailRoute") data class TodoDetailRoute(val todoId: String) : NavKey
@Serializable enum class SettingsPage { Diagnostics, Readiness, Notifications, PhoneData, Appearance, Devices, About, Pairing, Assistant, ClaudeCode, ModelConfig, Hubs, Persona, Plugins, Mail, Feishu, Channels }
@Serializable @SerialName("jarvis.core.navigation.SettingsPageRoute") data class SettingsPageRoute(val page: SettingsPage) : NavKey

enum class AppTab { Jarvis, Agents, Inbox, Settings }

interface Navigator {
    fun navigate(key: NavKey)
    fun back()
    fun selectTab(tab: AppTab)
}
