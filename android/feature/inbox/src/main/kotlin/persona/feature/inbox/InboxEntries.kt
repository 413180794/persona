package persona.feature.inbox

import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.CompositionLocalProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import persona.core.navigation.InboxRoute
import persona.core.navigation.Navigator
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

val LocalInboxRepository = staticCompositionLocalOf<InboxRepository?> { null }
internal val LocalInboxNavigator = staticCompositionLocalOf<Navigator?> { null }

@Serializable @SerialName("jarvis.feature.inbox.LarkRoute")
data class LarkRoute(val feedId: String, val itemId: String? = null, val startPosition: Long? = null) : NavKey

fun EntryProviderScope<NavKey>.inboxEntries(
    navigator: Navigator,
    repository: InboxRepository? = null,
    /** The hub's connectors (their threads and replies); null in 体验模式: a connector's item opens its detail page. */
    connectors: persona.core.contracts.api.ConnectorsManagement? = null,
    mailTransport: persona.core.contracts.api.MailTransport? = null,
    onMailAskAi: ((MailAiContext) -> Unit)? = null,
    onMailOpenLink: ((String) -> Unit)? = null,
) {
    entry<InboxRoute> {
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        InboxScreen(source, navigator)
    }
    entry<MailRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        val pairedMail by source.mailTransport.collectAsStateWithLifecycle()
        LarkLinkHost(source) {
            val opener = LocalLarkLinkOpener.current
            MailScreen(source, mailTransport ?: LocalMailTransport.current ?: pairedMail, route, navigator,
                onMailAskAi, onMailOpenLink ?: { url -> opener?.invoke(url, null); Unit })
        }
    }
    entry<LarkRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkScreen(source, route, navigator)
        }
    }
    entry<InboxItemDetailRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkLinkHost(source) { InboxItemDetailScreen(source, route, navigator) }
        }
    }
    entry<InboxEventRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkLinkHost(source) { InboxEventScreen(source, route, navigator) }
        }
    }
    entry<ConnectorThreadRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkLinkHost(source) {
                if (connectors == null) InboxItemDetailScreen(source, InboxItemDetailRoute(route.itemId), navigator)
                else ConnectorThreadScreen(source, connectors, route, navigator)
            }
        }
    }
    entry<LarkMomentRoute> { route ->
        val source = repository ?: LocalInboxRepository.current ?: injectedInboxRepository()
        CompositionLocalProvider(LocalInboxNavigator provides navigator) {
            LarkMomentsScreen(source, route, navigator)
        }
    }
}

@Serializable @SerialName("jarvis.feature.inbox.MailRoute")
data class MailRoute(val itemId: String) : NavKey

@Serializable @SerialName("jarvis.feature.inbox.LarkMomentRoute")
data class LarkMomentRoute(val postId: String, val itemId: String? = null) : NavKey

/** The own page of an item that is neither a chat, a 公司圈 post nor a mail: a Feishu document's notice, or an unknown item. */
@Serializable @SerialName("persona.feature.inbox.InboxItemDetailRoute")
data class InboxItemDetailRoute(val itemId: String) : NavKey

/** A hub connector's item opened as its conversation (ConnectorThread.kt): the thread its `connector` block names. */
@Serializable @SerialName("persona.feature.inbox.ConnectorThreadRoute")
data class ConnectorThreadRoute(val itemId: String) : NavKey

val LocalMailTransport = staticCompositionLocalOf<persona.core.contracts.api.MailTransport?> { null }
