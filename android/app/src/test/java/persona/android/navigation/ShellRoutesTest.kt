package persona.android.navigation

import androidx.navigation3.runtime.NavKey
import persona.core.domain.inbox.InboxItem
import persona.core.navigation.*
import persona.feature.inbox.InboxMomentRef
import persona.feature.inbox.InboxRecord
import persona.feature.inbox.LarkMomentRoute
import persona.feature.inbox.LarkRoute
import persona.feature.inbox.MailRoute
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ShellRoutesTest {
    private fun stacks() = mapOf<AppTab, MutableList<NavKey>>(
        AppTab.Jarvis to mutableListOf(AssistantRoute), AppTab.Agents to mutableListOf(AgentsRoute),
        AppTab.Inbox to mutableListOf(InboxRoute), AppTab.Settings to mutableListOf(SettingsRoute),
    )

    @Test fun notificationTabWinsBeforeMetadataAndLandsOnTheChat() {
        // Cold start: the session list has not loaded, so the router alone cannot tell the kind.
        val unknown = Router()
        val chat = ChatRoute("machine", "session")
        val assistant = unknown.open(NotificationRoute(AppTab.Jarvis, chat))!!
        val stacks = stacks()
        assertEquals(AppTab.Jarvis, assistant.restore(stacks))
        assertEquals(listOf(AssistantRoute, chat.copy(kind = ConversationKind.Assistant)), stacks.getValue(AppTab.Jarvis))

        // Metadata that disagrees does not move a conversation the notification placed explicitly.
        val agentMetadata = Router { _, _ -> ConversationKind.Agent }
        assertEquals(AppTab.Jarvis, agentMetadata.open(NotificationRoute(AppTab.Jarvis, chat))!!.tab)
        assertEquals(AppTab.Agents, unknown.open(NotificationRoute(AppTab.Agents, chat))!!.tab)
    }

    @Test fun notificationWithoutTabWaitsForMetadataAndTabOnlyLinksSelectTheTab() {
        val chat = ChatRoute("machine", "session")
        assertNull(Router().open(NotificationRoute(null, chat)))
        val known = Router { _, _ -> ConversationKind.Assistant }
        assertEquals(AppTab.Jarvis, known.open(NotificationRoute(null, chat))!!.tab)

        val stacks = stacks()
        stacks.getValue(AppTab.Agents).add(ChatRoute("machine", "other", ConversationKind.Agent))
        val tab = Router().open(NotificationRoute(AppTab.Agents))!!
        assertEquals(AppTab.Agents, tab.restore(stacks))
        // A tab-only link keeps that tab's stack as the user left it.
        assertEquals(2, stacks.getValue(AppTab.Agents).size)
        assertNull(Router().open(NotificationRoute(null)))
    }

    @Test fun inboxLinksOpenTheSourcePageOnlyOnceTheItemIsKnown() {
        val lark = InboxRecord(InboxItem("lark-item", kind = "dm"), feedId = "chat", startPosition = 42)
        val mail = InboxRecord(InboxItem("mail-item", kind = "mail"))
        val moment = InboxRecord(InboxItem("moment-item", kind = "moment"), moment = InboxMomentRef(postId = "post-1"))
        val records = listOf(lark, mail, moment)

        assertEquals(InboxDestination.Open(LarkRoute("chat", "lark-item", 42)), inboxDestination(InboxItemRoute("lark-item"), records, false))
        assertEquals(InboxDestination.Open(MailRoute("mail-item")), inboxDestination(InboxItemRoute("mail-item"), records, false))
        assertEquals(InboxDestination.Open(LarkMomentRoute("post-1", "moment-item")), inboxDestination(InboxItemRoute("moment-item"), records, false))
        // Before the first refresh a missing item may still arrive; afterwards it is reported.
        assertNull(inboxDestination(InboxItemRoute("gone"), records, false))
        assertEquals(InboxDestination.Missing, inboxDestination(InboxItemRoute("gone"), records, true))

        assertEquals(InboxDestination.Open(LarkRoute("chat", "lark-item", 42)), inboxDestination(LarkChatRoute("chat"), records, false))
        assertNull(inboxDestination(LarkChatRoute("other"), records, false))
        assertEquals(InboxDestination.Open(LarkRoute("other")), inboxDestination(LarkChatRoute("other"), records, true))
        assertNull(inboxDestination(InboxRoute, records, true))
    }

    @Test fun personaTerminalLinksRetainTheLegacyStack() {
        val link = "persona://open?target=terminal&machine=mac%20mini&session=a%2Bb"
        val route = terminalLink(link)!!
        val old = terminalLink(link.replaceFirst("persona://", "jarvis://"))!!
        assertEquals(old.key, route.key)
        assertEquals(old.tab, route.tab)
        val stacks = stacks()
        assertEquals(AppTab.Agents, route.open(stacks))
        assertEquals(listOf(AgentsRoute, ChatRoute("mac mini", "a+b", ConversationKind.Agent), TerminalRoute("mac mini", "a+b")),
            stacks.getValue(AppTab.Agents))
        assertNull(terminalLink("persona://open?target=terminal&machine=m&session=s&session=t"))
    }

    @Test fun terminalNotificationLinksOpenTheTerminalAboveItsConversation() {
        val route = terminalLink("jarvis://open?target=terminal&machine=mac%20mini&session=a%2Bb")!!
        assertEquals(TerminalRoute("mac mini", "a+b"), route.key)
        val stacks = stacks()
        assertEquals(AppTab.Agents, route.open(stacks))
        assertEquals(listOf(AgentsRoute, ChatRoute("mac mini", "a+b", ConversationKind.Agent), TerminalRoute("mac mini", "a+b")),
            stacks.getValue(AppTab.Agents))
        // Opening the same terminal again neither duplicates it nor the conversation.
        assertEquals(AppTab.Agents, route.open(stacks))
        assertEquals(3, stacks.getValue(AppTab.Agents).size)

        assertNull(terminalLink("jarvis://open?target=terminal&machine=m"))
        assertNull(terminalLink("jarvis://open?target=terminal&machine=m&session=s&session=t"))
        assertNull(terminalLink("jarvis://open?machine=m&session=s"))
        assertNull(terminalLink("https://example.com/open?target=terminal&machine=m&session=s"))
    }
}
