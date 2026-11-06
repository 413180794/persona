package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import org.junit.Assert.assertEquals
import org.junit.Test

/** A notification about a card outside any session lands on the Agents list, not on a chat left open there (#4808). */
class CardRouteTest {
    private fun stacks() = AppTab.entries.associateWith { mutableListOf<NavKey>(tabRoot(it)) }

    @Test fun aCardClosesTheConversationLeftOpenOnAgents() {
        val stacks = stacks()
        stacks.getValue(AppTab.Agents).add(ChatRoute("machine", "other", ConversationKind.Agent))
        stacks.getValue(AppTab.Persona).add(ChatRoute("machine", "assistant", ConversationKind.Assistant))
        val route = ResolvedRoute(AppTab.Agents, AgentsRoute, card = RequestRef("machine", "script"))
        assertEquals(AppTab.Agents, route.restore(stacks))
        assertEquals(listOf<NavKey>(AgentsRoute), stacks.getValue(AppTab.Agents))
        // Other tabs stay as the user left them.
        assertEquals(2, stacks.getValue(AppTab.Persona).size)
    }

    @Test fun aTabOnlyRouteStillKeepsTheStack() {
        val stacks = stacks()
        stacks.getValue(AppTab.Agents).add(ChatRoute("machine", "other", ConversationKind.Agent))
        ResolvedRoute(AppTab.Agents, AgentsRoute).restore(stacks)
        assertEquals(2, stacks.getValue(AppTab.Agents).size)
    }
}
