package persona.core.domain

import org.junit.Assert.*
import org.junit.Test
import persona.core.domain.views.AssistantOpen
import persona.core.domain.views.AssistantOpenTarget

class ProtocolCompatibilityTest {
    // Serialized message from baseline 39a706a338, with both kinds of structured attachment.
    private val oldText = """<jarvis-context kind="workspace" title="Project" url="jarvis://session/test-machine/test-session">Notes</jarvis-context>
Question
<jarvis-file kind="video" path="/workspace/demo.mp4" name="Demo" size="12"/>"""

    @Test fun baselineAndCurrentMessageMarkupRenderIdentically() {
        val current = oldText.replace("jarvis-context", "persona-context").replace("jarvis-file", "persona-file")
        val (oldContexts, oldPlain) = parseContexts(oldText)
        val (contexts, plain) = parseContexts(current)
        assertEquals(contexts, oldContexts)
        assertEquals(parseAttachedFiles(plain), parseAttachedFiles(oldPlain))
        assertEquals("Question", parseAttachedFiles(oldPlain).second)
        assertEquals(1, oldContexts.size)
        assertEquals(1, parseAttachedFiles(oldPlain).first.size)
    }

    @Test fun pairingAndCardLinksAcceptBothSchemesWithoutAcceptingOtherApps() {
        for (scheme in listOf("jarvis", "persona")) {
            assertEquals("test-token", Pairing.parse("$scheme://pair?url=http%3A%2F%2F127.0.0.1%3A18660&token=test-token")?.token)
            assertEquals(AssistantOpenTarget.Task("test-task"), AssistantOpen.target("$scheme://task/test-task"))
            assertNull(AssistantOpen.target("$scheme://user@example.com/task/test-task"))
        }
        assertNull(Pairing.parse("other://pair?url=http%3A%2F%2F127.0.0.1&token=test-token"))
        assertFalse(Pairing.isLink("jarvis:18660"))
    }

    @Test fun oldMcpCallsHaveTheSamePersonaVerbAsNewCalls() {
        assertTrue(isPersonaTool("mcp__plugin_jarvis__ask_jarvis"))
        assertEquals(namedToolVerb("mcp__persona__ask_persona"), namedToolVerb("mcp__jarvis__ask_jarvis"))
        assertEquals(namedToolVerb("mcp__persona__persona_settings"), namedToolVerb("mcp__jarvis__jarvis_settings"))
    }
}
