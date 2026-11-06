package persona.core.domain.rewind

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Item
import persona.core.contracts.Session
import persona.core.contracts.api.DeliveryState
import persona.core.contracts.api.RewindTarget

/** "Rewind to here"'s target and when it is off (web lib/rewind.test.ts). */
class RewindTest {
    private fun user(id: String, text: String?, kind: String = "text") = Item(id, role = "user", kind = kind, text = text)

    @Test fun aMessageIsCountedFromTheEndAmongTheSameWords() {
        val items = listOf(
            user("a#0", "跑测试"), Item("r#0", role = "assistant", kind = "text", text = "好"),
            user("b#0", "改一下"), user("b#1", "再看看"), // one transcript entry: one message
            user("c#0", "跑  测试"), // the same words, spaces aside
            user("q", "排队中", kind = "text").copy(queued = true),
            user("img#0", null, kind = "image"),
        )
        assertEquals(listOf("跑测试", "改一下\n再看看", "跑  测试", ""), Rewind.prompts(items).map { it.raw })
        assertEquals(RewindTarget("跑测试", occurrence = 2, promptsAfter = 3), Rewind.target(items, "a#0"))
        assertEquals(RewindTarget("改一下\n再看看", 1, 2), Rewind.target(items, "b#0"))
        assertNull(Rewind.target(items, "img#0")) // photos alone have no words to name it by
        assertNull(Rewind.target(items, "missing"))
    }

    @Test fun onlyAnIdleClaudeSessionCanRewind() {
        val idle = Session("m", "s", status = "idle", state = "idle")
        assertNull(Rewind.block(idle, busy = false))
        assertEquals(Rewind.Block.Working, Rewind.block(idle, busy = true))
        assertEquals(Rewind.Block.Working, Rewind.block(idle.copy(status = "working", state = "working"), false))
        assertEquals(Rewind.Block.NeedsYou, Rewind.block(idle.copy(pendingInteractions = 1), false))
        assertEquals(Rewind.Block.Offline, Rewind.block(idle.copy(status = "offline", state = "offline"), false))
        assertEquals(Rewind.Block.ClaudeOnly, Rewind.block(idle.copy(agent = "codex"), false))
        assertEquals(Rewind.Block.ClaudeOnly, Rewind.block(idle.copy(assistant = true), false))
        assertEquals(Rewind.Block.ClaudeOnly, Rewind.block(null, false))
    }

    @Test fun aFailedMessageDoesNotHoldTheRewind() {
        assertFalse(Rewind.outgoing(emptyList()))
        assertFalse(Rewind.outgoing(listOf(DeliveryState.Rejected, DeliveryState.Refused)))
        assertTrue(Rewind.outgoing(listOf(DeliveryState.Refused, DeliveryState.Sending)))
        assertTrue(Rewind.outgoing(listOf(DeliveryState.Queued)))
    }

    @Test fun onlyATranscriptLineCanBeBranchedOff() {
        assertTrue(Rewind.forkable("6f1c2a#3"))
        assertFalse(Rewind.forkable("line12"))
        assertFalse(Rewind.forkable("no-separator"))
    }
}
