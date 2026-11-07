package persona.feature.assistant

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import persona.core.contracts.SessionKey
import persona.core.contracts.api.AssistantSnapshot

/** A home-composer message for another manager goes to that manager's main conversation and nowhere else. */
class ManagerMainTargetTest {
    private val xMain = SessionKey("mac", "x-main")

    @Test fun theSnapshotCountsOnlyWhenItIsThatManagers() = runBlocking {
        val asked = mutableListOf<String>()
        // The page's snapshot is m-3's: its main conversation, nothing asked.
        assertEquals(xMain, managerMainTarget("m-3", AssistantSnapshot(xMain, manager = "m-3")) { asked += it; error("unused") })
        assertTrue(asked.isEmpty())
        // Still the default's (switched a moment ago) or another one's: asked of m-3 itself.
        assertEquals(xMain, managerMainTarget("m-3", AssistantSnapshot(SessionKey("hub", "d-main"))) { asked += it; xMain })
        assertEquals(xMain, managerMainTarget("m-3", AssistantSnapshot(SessionKey("mac", "y"), manager = "m-4")) { asked += it; xMain })
        assertEquals(listOf("m-3", "m-3"), asked)
    }

    @Test fun notFoundFailsInsteadOfGoingElsewhere() = runBlocking {
        try {
            managerMainTarget("m-3", AssistantSnapshot(SessionKey("hub", "d-main"))) { throw IllegalStateException("m-3 is offline") }
            fail("expected a failure")
        } catch (e: IllegalStateException) { assertEquals("m-3 is offline", e.message) }
    }
}
