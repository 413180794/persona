package jarvis.feature.inbox

import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.CompositionLocalProvider
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation3.runtime.EntryProviderScope
import androidx.navigation3.runtime.NavKey
import jarvis.core.navigation.InboxRoute
import jarvis.core.navigation.Navigator
import kotlinx.serialization.Serializable

val LocalInboxRepository = staticCompositionLocalOf<InboxRepository?> { null }
internal val LocalInboxNavigator = staticCompositionLocalOf<Navigator?> { null }

@Serializable
data class LarkRoute(val feedId: String, val itemId: String? = null, val startPosition: Long? = null) : NavKey

fun EntryProviderScope<NavKey>.inboxEntries(
    navigator: Navigator,
    repository: InboxRepository? = null,
    mailTransport: jarvis.core.contracts.api.MailTransport? = null,
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
}

@Serializable
data class MailRoute(val itemId: String) : NavKey

val LocalMailTransport = staticCompositionLocalOf<jarvis.core.contracts.api.MailTransport?> { null }
