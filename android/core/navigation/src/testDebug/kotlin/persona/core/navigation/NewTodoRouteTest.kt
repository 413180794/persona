package persona.core.navigation

import androidx.navigation3.runtime.NavKey
import org.junit.Assert.*
import org.junit.Test

/** The launcher's "New to-do" opens the sheet every time, also when the list it opens is still on the stack (#5718). */
class NewTodoRouteTest {
    private var now = 1_000L
    private val requests = NewTodoRequests { now }
    private val stacks = AppTab.entries.associateWith { mutableListOf<NavKey>(tabRoot(it)) }
    private val link = Router().parse(RouteInput.DeepLink(NewTodoLink))!!

    private fun taken(): Boolean = requests.requests.value?.let(requests::take) ?: false

    @Test fun everyUseOfTheLinkAsksForTheSheet() {
        assertEquals(AppTab.Agents, link.restore(stacks, requests))
        assertTrue("first use: no New to-do request", taken())
        // The sheet was closed and the app left; the second use finds the list it opened still there.
        now += 60_000
        assertEquals(AppTab.Agents, link.restore(stacks, requests))
        assertEquals(listOf<NavKey>(AgentsRoute, NewTodoRoute), stacks.getValue(AppTab.Agents))
        assertTrue("second use: no New to-do request", taken())
        // Also after a to-do was opened from that list.
        stacks.getValue(AppTab.Agents).add(TodoDetailRoute("todo"))
        link.restore(stacks, requests)
        assertEquals(listOf<NavKey>(AgentsRoute, NewTodoRoute), stacks.getValue(AppTab.Agents))
        assertTrue("after a to-do's page: no New to-do request", taken())
    }

    @Test fun otherRoutesAskForNothing() {
        ResolvedRoute(AppTab.Agents, AgentsRoute).restore(stacks, requests)
        Router().parse(RouteInput.DeepLink("persona://open?target=inbox&item=item"))!!.restore(stacks, requests)
        assertNull(requests.requests.value)
    }

    @Test fun aRequestIsTakenOnceAndLapses() {
        requests.request()
        val at = requests.requests.value!!
        assertTrue(requests.take(at))
        assertFalse(requests.take(at))
        // Nobody took it in time: by the time something asks, it is too old to act on.
        requests.request()
        now += 15_001
        assertFalse(taken())
        assertNull(requests.requests.value)
    }
}
