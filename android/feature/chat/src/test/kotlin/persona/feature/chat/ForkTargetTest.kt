package persona.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.SessionKey
import persona.core.navigation.ChatRoute
import persona.core.navigation.ConversationKind

/** "Branch to new chat" opens an assistant's branch as an assistant conversation (web `forkAt`), an agent's as an agent chat. */
class ForkTargetTest {
    private val key = SessionKey("box", "branch-1")

    @Test fun anAssistantBranchOpensAsAnAssistantConversation() {
        var opened: Pair<String, SessionKey>? = null
        val chrome = ChatChromeBindings(onOpenSession = { opened = "agent" to it }, onOpenAssistant = { opened = "assistant" to it })
        chrome.forkOpener(ChatRenderStyle.Assistant)!!.invoke(key)
        assertEquals("assistant" to key, opened)
        chrome.forkOpener(ChatRenderStyle.Agent)!!.invoke(key)
        assertEquals("agent" to key, opened)
    }

    @Test fun withoutAnAssistantPageTheAssistantHasNoBranch() {
        assertNull(ChatChromeBindings(onOpenSession = {}).forkOpener(ChatRenderStyle.Assistant))
    }

    @Test fun theRoutesKind() {
        assertEquals(ChatRoute("box", "branch-1", kind = ConversationKind.Assistant), conversationRoute(key, assistant = true))
        assertEquals(ChatRoute("box", "branch-1"), conversationRoute(key))
    }
}
