package persona.feature.agents

import androidx.navigation3.runtime.NavKey
import persona.core.contracts.SessionKey
import persona.core.navigation.*
import org.junit.Assert.*
import org.junit.Test

class AgentSelectionTest {
    private val session = SessionKey("machine", "session")

    @Test fun chatInteractionAndTerminalSelectBySessionIdentity() {
        val chat = ChatRoute("machine", "session")
        val routes = listOf<NavKey>(chat,
            chat.copy(kind = ConversationKind.Agent, source = RouteSource.InboxItem("item")),
            chat.copy(kind = ConversationKind.Assistant, source = RouteSource.LarkConversation("feed")),
            InteractionRoute(chat.copy(source = RouteSource.Interaction("machine", "request")), "request"),
            TerminalRoute("machine", "session"))
        routes.forEach {
            assertTrue(isSessionSelected(it, session))
            assertFalse(isSessionSelected(it, SessionKey("other", "session")))
            assertFalse(isSessionSelected(it, SessionKey("machine", "other")))
        }
    }

    @Test fun rootAndOtherDetailsDoNotSelectASession() {
        listOf(null, AgentsRoute, SettingsPageRoute(SettingsPage.Devices)).forEach {
            assertFalse(isSessionSelected(it, session))
        }
    }
}
