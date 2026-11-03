package persona.android.ui

import androidx.compose.runtime.*
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import persona.core.contracts.api.MainConversationSender
import persona.core.designsystem.AppFeedback
import persona.core.navigation.AppTab
import persona.core.navigation.ChatRoute
import persona.core.navigation.Navigator
import persona.feature.chat.*
import persona.feature.inbox.InboxWebSelection
import persona.feature.inbox.LocalInboxWebSelection
import java.util.UUID

/**
 * Links for the whole shell: the link sheet over pages outside a conversation (the knowledge base, the Persona home's
 * own links), `jarvis://` links tapped in any conversation (they open a conversation or a tab, nothing else), and
 * text picked on a page with no conversation behind it (those pages, and the inbox's): 交给 Persona puts it in Persona's
 * main conversation and shows the Persona tab, as the inbox's own 交给 Persona does.
 */
@Composable
internal fun ShellLinkHost(session: ShellSession, navigator: Navigator, feedback: AppFeedback, content: @Composable () -> Unit) {
    // The paired sender is the connection's forwarding wrapper, not the outbox itself, so it is no
    // MainConversationSender: the main conversation comes from the pairing's chat wiring, as in a chat (ShellChat).
    // Before, 交给 Persona never showed on these pages (only 体验模式's sender is one).
    val activeWiring by chatWiringSource().current.collectAsStateWithLifecycle()
    val main = session.sender as? MainConversationSender
        ?: activeWiring?.takeIf { it.scope.path == session.identity }?.mainSender
    val handToPersona: (suspend (String) -> Unit)? = remember(main, navigator) {
        main?.let { sender ->
            val send: suspend (String) -> Unit = { message ->
                sender.sendToMain(message, UUID.randomUUID().toString())
                navigator.selectTab(AppTab.Jarvis)
            }
            send
        }
    }
    val appLinks: (ChatAppLink) -> Unit = remember(navigator) {
        { link: ChatAppLink ->
            when (link) {
                is ChatAppLink.Session -> navigator.navigate(ChatRoute(link.machineId, link.sessionId))
                is ChatAppLink.Tab -> navigator.selectTab(when (link.tab) {
                    "agents" -> AppTab.Agents
                    "inbox" -> AppTab.Inbox
                    else -> AppTab.Jarvis
                })
            }
        }
    }
    var picked by remember { mutableStateOf<ChatLinkContext?>(null) }
    val inboxPick: (InboxWebSelection) -> Unit = { selection -> picked = ChatLinkContext("context", selection.title, selection.url, selection.text) }
    // Without a main conversation to send to, a pick can only say where it works.
    val contextElsewhere: (ChatLinkContext) -> Unit = { feedback.notice("请在对话中打开链接后选取上下文") }
    val editElsewhere: suspend (ChatLinkContext, String) -> Unit = { _, _ -> feedback.notice("请在对话中修改选中的内容") }
    CompositionLocalProvider(LocalChatAppLinks provides appLinks,
        LocalInboxWebSelection provides inboxPick.takeIf { handToPersona != null }) {
        ChatLinkHost(
            onContext = contextElsewhere.takeIf { handToPersona == null },
            onEdit = editElsewhere.takeIf { handToPersona == null },
            onHandToPersona = handToPersona,
        ) { _ -> content() }
    }
    picked?.let { selection ->
        HandToPersonaDialog(selection, onDismiss = { picked = null }) { message ->
            handToPersona?.invoke(message)
            picked = null
        }
    }
}
