package persona.feature.assistant

import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.SessionKey

class StarterTargetTest {
    @Test fun aRetryReusesTheConversationAndAnotherLauncherStartsItsOwn() = runTest {
        val started = mutableListOf<String>()
        val start: suspend (String) -> SessionKey = { launcher -> started += launcher; SessionKey("mac", "c${started.size}") }

        // First try on A: one new conversation; the message then failed, and the retry goes to that same one.
        val first = starterTarget("A", null, start)
        assertEquals(StarterStart("A", SessionKey("mac", "c1")), first)
        assertEquals(first, starterTarget("A", first, start))
        assertEquals(listOf("A"), started)

        // Changed to B since: a conversation of its own on B, not A's.
        assertEquals(StarterStart("B", SessionKey("mac", "c2")), starterTarget("B", first, start))
        assertEquals(listOf("A", "B"), started)
    }

    @Test fun whatAStartLeftSurvivesBeingSaved() {
        val start = StarterStart("cc_x", SessionKey("mac", "s/1"))
        val saved = with(StarterStartSaver) { androidx.compose.runtime.saveable.SaverScope { true }.save(start) }
        assertEquals(start, StarterStartSaver.restore(saved!!))
        assertNull(with(StarterStartSaver) { androidx.compose.runtime.saveable.SaverScope { true }.save(null) })
    }
}
