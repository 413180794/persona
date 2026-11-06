package persona.android.ui

import androidx.navigation3.runtime.NavKey
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AssistantSnapshot
import persona.core.navigation.*

class ShellHubLinksTest {
    private val hub = "https://hub.example.com"
    private val opened = mutableListOf<NavKey>()
    private val tabs = mutableListOf<AppTab>()
    private val navigator = object : Navigator {
        override fun navigate(key: NavKey) { opened += key }
        override fun back() = Unit
        override fun selectTab(tab: AppTab) { tabs += tab }
    }
    private val assistant = AssistantSnapshot(SessionKey("hub-m", "main-1"), listOf(Session(machineId = "hub-m", sessionId = "side-1")))

    private fun open(url: String, namespace: String? = null, sessions: List<SessionKey> = emptyList(), snapshot: AssistantSnapshot = assistant) =
        openHubPage(url, listOf(hub), namespace, sessions, snapshot, navigator)

    @Test fun anAgentLinkOpensItsConversation() {
        assertTrue(open("$hub/s/m1/abc-1?view=chat"))
        assertEquals(listOf<NavKey>(ChatRoute("m1", "abc-1")), opened)
    }

    @Test fun aLinkToAMessageLeavesItForItsChat() {
        MessageJumps.take("m1~gcafe00a1", "abc-123")
        val sessions = listOf(SessionKey("m1~gcafe00a1", "abc-123"))
        assertTrue(open("$hub/s/m1/abc?at=item%231", namespace = "gcafe00a1", sessions = sessions))
        assertEquals(listOf<NavKey>(ChatRoute("m1~gcafe00a1", "abc-123")), opened)
        assertEquals(MessageJumps.Jump("m1~gcafe00a1", "abc-123", "item#1"), MessageJumps.pending)
        assertTrue(open("$hub/persona/side-1?at=item-2"))
        assertEquals(MessageJumps.Jump("hub-m", "side-1", "item-2"), MessageJumps.pending)
        assertEquals("item-2", MessageJumps.take("hub-m", "side-1"))
        // no message: nothing waits
        assertTrue(open("$hub/s/m1/abc-1"))
        assertNull(MessageJumps.pending)
    }

    @Test fun anotherGroupsMachinesCarryItsSuffix() {
        assertTrue(open("$hub/s/m1/abc-1", namespace = "gcafe00a1"))
        assertTrue(open("$hub/machines/m1", namespace = "gcafe00a1"))
        assertEquals(listOf(ChatRoute("m1~gcafe00a1", "abc-1"), MachineDetailRoute("m1~gcafe00a1")), opened)
    }

    @Test fun aShortSessionIdOpensTheOneSessionItNames() {
        val sessions = listOf(SessionKey("m1", "abc-123"), SessionKey("m1", "def-456"), SessionKey("m2", "abc-999"))
        assertTrue(open("$hub/s/m1/abc", sessions = sessions))
        // two sessions start so: the link's id as it is
        assertTrue(open("$hub/s/m1/abc", sessions = sessions + SessionKey("m1", "abc-777")))
        assertEquals(listOf(ChatRoute("m1", "abc-123"), ChatRoute("m1", "abc")), opened)
    }

    @Test fun assistantConversationsOpenInThePersonaHome() {
        assertTrue(open("$hub/persona/side-1"))
        assertTrue(open("$hub/persona/main-1"))
        assertTrue(open("$hub/"))
        // not listed yet: on the assistant's machine
        assertTrue(open("$hub/persona/new-2"))
        assertEquals(listOf(
            ChatRoute("hub-m", "side-1", ConversationKind.Assistant),
            ChatRoute("hub-m", "main-1", ConversationKind.Assistant),
            ChatRoute("hub-m", "main-1", ConversationKind.Assistant),
            ChatRoute("hub-m", "new-2", ConversationKind.Assistant),
        ), opened)
    }

    @Test fun beforeTheAssistantIsKnown() {
        assertTrue(open("$hub/persona", snapshot = AssistantSnapshot()))
        assertEquals(listOf(AppTab.Persona), tabs)
        // a conversation whose machine is not known yet stays a web page
        assertFalse(open("$hub/persona/side-1", snapshot = AssistantSnapshot()))
        assertTrue(opened.isEmpty())
    }

    @Test fun tasksAndPages() {
        assertTrue(open("$hub/todos?id=todo_1"))
        assertTrue(open("$hub/inbox/item-1"))
        assertTrue(open("$hub/agents"))
        assertEquals(listOf(TodoDetailRoute("todo_1"), InboxItemRoute("item-1"), AgentsRoute), opened)
    }

    @Test fun otherLinksAreLeftToTheSheet() {
        assertFalse(open("https://other.example.com/s/m1/abc-1"))
        assertFalse(open("$hub/kb/people/x"))
        assertFalse(open("https://hub.example.com:8443/s/m1/abc-1"))
        assertTrue(opened.isEmpty() && tabs.isEmpty())
    }
}
