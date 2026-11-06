package persona.android.di

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertEquals
import org.junit.Test
import persona.core.contracts.HubDesktop
import persona.core.contracts.api.ConnectionState

/** "Signed-in computers" with two hub groups: only the selected group's `desktops` list shows (#3048 review). */
class GroupDesktopsTest {
    private val mine = listOf(HubDesktop("d1", "Mac A"))
    private val theirs = listOf(HubDesktop("d2", "Mac B"))

    private fun withDesktops(block: suspend (GroupDesktops, MutableStateFlow<String?>) -> Unit): Unit = runBlocking {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Unconfined)
        val selected = MutableStateFlow<String?>("a")
        try { block(GroupDesktops(scope, selected), selected) } finally { scope.cancel() }
    }

    private suspend fun GroupDesktops.awaitShown(expected: List<HubDesktop>) =
        withTimeout(2_000) { selected.first { it == expected } }

    @Test fun anotherGroupsListDoesNotReplaceTheSelectedOne() = withDesktops { desktops, _ ->
        desktops.onDesktops("a", mine)
        desktops.onDesktops("b", theirs)
        desktops.awaitShown(mine)
        assertEquals(mine, desktops.selected.value)
        Unit
    }

    @Test fun switchingGroupsShowsTheNewGroupsList() = withDesktops { desktops, selected ->
        desktops.onDesktops("a", mine)
        desktops.onDesktops("b", theirs)
        selected.value = "b"
        desktops.awaitShown(theirs)
        selected.value = "c"
        desktops.awaitShown(emptyList())
    }

    @Test fun aGroupThatDisconnectsForgetsItsListButUpdatingKeepsIt() = withDesktops { desktops, _ ->
        desktops.onDesktops("a", mine)
        desktops.onState("a", ConnectionState.Updating)
        desktops.awaitShown(mine)
        desktops.onState("a", ConnectionState.Connecting)
        desktops.awaitShown(emptyList())
    }

    @Test fun groupsThatAreGoneAreDropped() = withDesktops { desktops, selected ->
        desktops.onDesktops("b", theirs)
        desktops.retain(setOf("a"))
        selected.value = "b"
        desktops.awaitShown(emptyList())
    }
}
