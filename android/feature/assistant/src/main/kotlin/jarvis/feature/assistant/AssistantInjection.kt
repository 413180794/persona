package jarvis.feature.assistant

import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import dagger.Module
import dagger.Provides
import javax.inject.Singleton
import jarvis.core.data.HubDataStore
import jarvis.core.contracts.api.DraftSnapshot
import jarvis.core.contracts.api.MainConversationSender
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.AssistantManagement
import jarvis.core.contracts.api.SessionsRepository
import jarvis.core.contracts.api.AssistantConversations
import jarvis.core.contracts.api.MessageSender
import jarvis.core.navigation.Navigator
import jarvis.feature.chat.chatWiringSource
import jarvis.core.contracts.api.ChatComposerService
import jarvis.core.contracts.api.DraftRepository
import kotlinx.coroutines.CoroutineScope

data class AssistantHostBindings(
    val identity: String,
    val management: AssistantManagement,
    val drafts: DraftRepository,
    val coroutineScope: CoroutineScope,
    val phoneToolsAvailable: Boolean?,
    val machines: List<jarvis.core.contracts.Machine>,
    val mainSender: MainConversationSender,
    val chatContent: @Composable (SessionKey, AssistantChatOptions) -> Unit,
)

val LocalAssistantHost = staticCompositionLocalOf<AssistantHostBindings?> { null }

@Composable
internal fun assistantComposerService(): ChatComposerService? {
    val host = LocalAssistantHost.current ?: return null
    val wiring by chatWiringSource().current.collectAsStateWithLifecycle()
    return wiring?.takeIf { it.scope.path == host.identity }?.composer
}

interface AssistantAppWiring {
    @Composable fun Host(navigator: Navigator, content: @Composable () -> Unit)
}

@Composable
internal fun AssistantAppHost(navigator: Navigator, content: @Composable () -> Unit) {
    if (LocalAssistantHost.current != null) { content(); return }
    val context = LocalContext.current.applicationContext
    val dependencies = remember(context) { EntryPointAccessors.fromApplication(context, AssistantEntryPoint::class.java) }
    dependencies.assistantWiring().Host(navigator, content)
}

@EntryPoint
@InstallIn(SingletonComponent::class)
interface AssistantEntryPoint {
    fun assistantConversations(): AssistantConversations
    fun messageSender(): MessageSender
    fun mainConversationSender(): MainConversationSender
    fun sessionsRepository(): SessionsRepository
    fun assistantWiring(): AssistantAppWiring
}

@Composable
fun ConnectedAssistant(
    navigator: Navigator,
    management: AssistantManagement? = null,
    phoneToolsAvailable: Boolean? = null,
    requestedSession: SessionKey? = null,
    assistantChatContent: (@Composable (SessionKey, AssistantChatOptions) -> Unit)? = null,
) = AssistantAppHost(navigator) {
    val context = LocalContext.current.applicationContext
    val dependencies = remember(context) { EntryPointAccessors.fromApplication(context, AssistantEntryPoint::class.java) }
    val repository = remember(dependencies) { dependencies.assistantConversations() }
    val pins by repository.pinned.collectAsStateWithLifecycle()
    val sessions = remember(dependencies) { dependencies.sessionsRepository() }
    val machines by sessions.machines.collectAsStateWithLifecycle()
    AssistantScreen(navigator, repository, dependencies.messageSender(), repository::startConversation, pins,
        machines = machines, phoneToolsAvailable = phoneToolsAvailable ?: LocalAssistantHost.current?.phoneToolsAvailable,
        management = management ?: LocalAssistantHost.current?.management,
        requestedSession = requestedSession, assistantChatContent = assistantChatContent ?: LocalAssistantHost.current?.chatContent,
        mainConversationSender = dependencies.mainConversationSender())
}

/** All assistant entry points reuse the pairing-owned outbox, including the platform share queue. */
@Module
@InstallIn(SingletonComponent::class)
object AssistantSendingModule {
    @Provides
    @Singleton
    fun mainConversationSender(data: HubDataStore, messages: MessageSender): MainConversationSender =
        object : MainConversationSender {
            override val outbox = messages.outbox

            override suspend fun sendToMain(text: String, clientMessageId: String, draft: DraftSnapshot?) {
                val pairing = checkNotNull(data.restore()) { "请先配对 hub" }
                pairing.messages.sendToMain(text, clientMessageId, draft)
            }
        }
}
