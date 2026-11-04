package persona.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Session

/** The chat's strip of its space's tabs (iOS `AppModel.siblings`, `Session.slotLabel`). */
class ChatSiblingTabsTest {
    private fun s(id: String, space: String? = "ws1", slot: Int? = null, name: String? = null, pane: String? = "p-$id",
                  status: String = "idle", at: String = "2026-11-04T08:00:00Z", machine: String = "mac", assistant: Boolean = false) =
        Session(machineId = machine, sessionId = id, spaceId = space, slotIndex = slot, slotName = name, paneId = pane,
            status = status, updatedAt = at, assistant = assistant)

    @Test fun theSameSpacesLiveTabsInTabOrder() {
        val a = s("a", slot = 1)
        val all = listOf(
            s("b", slot = 2), a, s("c", slot = 3, status = "offline"), // offline: not a tab
            s("d", space = "ws2", slot = 1), s("e", machine = "box2", slot = 1), // another space, another machine
            s("j", slot = 0, assistant = true), // the assistant is never a tab
        )
        assertEquals(listOf("a", "b"), spaceTabs(a, all).map { it.sessionId })
        // The session itself counts even before the list has it.
        assertEquals(listOf("a", "b"), spaceTabs(a, all - a).map { it.sessionId })
        assertTrue(spaceTabs(s("x", space = null), listOf(s("y", space = null))).isEmpty())
        assertTrue(spaceTabs(null, all).isEmpty())
    }

    @Test fun onePerPaneTheOpenOneThenTheLatest() {
        // /clear left the pane listed under its old id for a moment.
        val old = s("old", slot = 1, pane = "p1", at = "2026-11-04T08:00:00Z")
        val new = s("new", slot = 1, pane = "p1", at = "2026-11-04T09:00:00Z")
        val other = s("other", slot = 2, pane = "p2")
        assertEquals(listOf("new", "other"), spaceTabs(other, listOf(old, new, other)).map { it.sessionId })
        assertEquals(listOf("old", "other"), spaceTabs(old, listOf(old, new, other)).map { it.sessionId })
    }

    @Test fun tabNames() {
        assertEquals("api", tabLabel(s("a", slot = 1, name = "api")))
        assertEquals("#2", tabLabel(s("a", slot = 2, name = "2"))) // herdr's default number
        assertEquals("#3", tabLabel(s("a", slot = 3)))
        assertEquals("a", tabLabel(Session(sessionId = "a", title = "a")))
    }
}
