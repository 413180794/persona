package jarvis.android

import jarvis.android.data.BackgroundTask
import jarvis.android.data.Grouper
import jarvis.android.data.LoopInfo
import jarvis.android.data.Machine
import jarvis.android.data.Pins
import jarvis.android.data.Rank
import jarvis.android.data.Session
import jarvis.android.data.Subagent
import jarvis.android.data.isWaiting
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** The list order must match the iOS app's (ChatsView.swift `rank`, `ordered`, `SpaceGroup.build`, 按项目). */
class GroupingTest {
    private fun s(
        id: String, state: String, space: String? = "app", updated: String = "2026-10-30T10:00:00Z",
        machine: String = "server", slot: Int = 0, loop: Boolean = false, bg: Boolean = false, sub: Boolean = false,
        status: String = "idle",
    ) = Session(
        machineId = machine, sessionId = id, state = state, status = status, spaceId = space?.let { "w-$it" }, spaceName = space,
        slotIndex = slot, updatedAt = updated, loop = if (loop) LoopInfo(kind = "wakeup") else null,
        background = if (bg) listOf(BackgroundTask(id = "b")) else null, subagents = if (sub) listOf(Subagent("a", running = true)) else null,
    )

    private val plain = Grouper(Pins(), Pins())

    @Test fun rankOrder() {
        val list = listOf(
            s("seen", "idle"), s("working", "working"), s("unread", "unread"), s("needs", "needs_you"),
            s("waiting", "idle", loop = true),
        )
        assertEquals(listOf("needs", "unread", "working", "waiting", "seen"), plain.ordered(list).map { it.sessionId })
    }

    @Test fun waitingNeedsALoopOrBackgroundWork() {
        assertFalse(s("a", "idle").isWaiting)
        assertTrue(s("a", "idle", loop = true).isWaiting)
        assertTrue(s("a", "idle", bg = true).isWaiting)
        assertTrue(s("a", "idle", sub = true).isWaiting)
        assertFalse(s("a", "working", loop = true).isWaiting) // working is not quiet
    }

    @Test fun mutedRanksAsMutedButNeedsYouStaysFirst() {
        val muted = Grouper(Pins(), Pins(sessions = listOf("server/w", "server/n")))
        assertEquals(Rank.Muted, muted.rank(s("w", "working")))
        assertEquals(Rank.NeedsYou, muted.rank(s("n", "needs_you")))
        // Muted and seen is just seen.
        assertEquals(Rank.Seen, Grouper(Pins(), Pins(sessions = listOf("server/x"))).rank(s("x", "idle")))
    }

    @Test fun newestFirstWithinARank() {
        val list = listOf(s("old", "working", updated = "2026-10-30T09:00:00Z"), s("new", "working", updated = "2026-10-30T11:00:00Z"))
        assertEquals(listOf("new", "old"), plain.ordered(list).map { it.sessionId })
    }

    @Test fun spacesKeepTabOrderInside() {
        val g = plain.spaces(listOf(s("b", "working", slot = 2), s("a", "idle", slot = 1)))
        assertEquals(1, g.size)
        assertEquals(listOf("a", "b"), g[0].sessions.map { it.sessionId })
    }

    @Test fun spaceLayout() {
        val list = listOf(
            s("q1", "idle", space = "quiet"),
            s("b1", "working", space = "busy"),
            s("m1", "working", space = "muted"),
            s("p1", "idle", space = "pinned"),
        )
        val g = Grouper(Pins(spaces = listOf("server/pinned")), Pins(spaces = listOf("server/muted")))
        val layout = g.spaceLayout(list)
        // Pinned first (even when quiet), then busy, then muted-but-running; quiet ones fold away.
        assertEquals(listOf("pinned", "busy", "muted"), layout.active.map { it.name })
        assertEquals(listOf("quiet"), layout.quiet.map { it.name })
    }

    @Test fun pinnedAndMutedSpaceIsTreatedAsMuted() {
        val g = Grouper(Pins(spaces = listOf("server/x")), Pins(spaces = listOf("server/x")))
        val layout = g.spaceLayout(listOf(s("a", "idle", space = "x")))
        assertEquals(emptyList<String>(), layout.active.map { it.name })
        assertEquals(listOf("x"), layout.quiet.map { it.name })
    }

    @Test fun offlineSessionsHaveNoSpace() {
        val g = plain.spaces(listOf(s("off", "offline", status = "offline"), s("on", "idle")))
        assertEquals(listOf("on"), g.flatMap { it.sessions }.map { it.sessionId })
    }

    @Test fun staysOutOfTheIdleFold() {
        val g = Grouper(Pins(sessions = listOf("server/pinned")), Pins())
        assertTrue(g.stays(s("w", "working")))
        assertTrue(g.stays(s("pinned", "idle")))
        assertTrue(g.stays(s("loop", "idle", loop = true)))
        assertFalse(g.stays(s("idle", "idle")))
    }

    @Test fun byStateAndByMachine() {
        val list = listOf(s("a", "working"), s("b", "idle", machine = "laptop"), s("c", "needs_you"))
        assertEquals(listOf(Rank.NeedsYou, Rank.Working, Rank.Seen), plain.byState(list).map { it.first })
        val machines = listOf(Machine(id = "laptop", name = "笔记本"), Machine(id = "server", name = "服务器"), Machine(id = "empty"))
        // Machines in the hub's order; a machine with no agents gets no section.
        assertEquals(listOf("laptop", "server"), plain.byMachine(list, machines).map { it.first.id })
        assertEquals(listOf("c", "a"), plain.byMachine(list, machines)[1].second.map { it.sessionId })
    }
}
