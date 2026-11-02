package jarvis.feature.knowledge

import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import jarvis.core.contracts.api.*
import jarvis.core.navigation.Navigator
import kotlinx.serialization.Serializable
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent

@Serializable data object KnowledgeRoute : NavKey
@Serializable data class KnowledgeDetailRoute(val kind: KnowledgeKind, val id: String) : NavKey

@EntryPoint
@InstallIn(SingletonComponent::class)
interface KnowledgeSendingEntryPoint {
    fun mainConversationSender(): MainConversationSender
}

/** The host must route links to its in-app reader, never to an external ACTION_VIEW intent. */
fun EntryProviderScope<NavKey>.knowledgeEntries(
    navigator: Navigator,
    connection: KnowledgeConnection,
    @Suppress("UNUSED_PARAMETER") assistant: AssistantConversations,
    sender: MessageSender,
    openLinkInApp: (String) -> Unit,
) {
    entry<KnowledgeRoute> {
        val model = viewModel { KnowledgeViewModel(connection) }
        KnowledgeScreen(model, navigator)
    }
    entry<KnowledgeDetailRoute> { route ->
        val context = LocalContext.current.applicationContext
        val mainSender = remember(sender, context) {
            sender as? MainConversationSender ?: EntryPointAccessors.fromApplication(
                context, KnowledgeSendingEntryPoint::class.java,
            ).mainConversationSender()
        }
        val model = viewModel(key = "knowledge/${route.kind.wire}/${route.id}") {
            KnowledgeDetailViewModel(route.kind, route.id, connection, mainSender)
        }
        KnowledgeDetailScreen(model, navigator, openLinkInApp)
    }
}
