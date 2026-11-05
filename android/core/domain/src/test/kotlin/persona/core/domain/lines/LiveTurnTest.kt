package persona.core.domain.lines

import persona.core.contracts.Item
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** #3350: while the agent works, the last turn has a live work line, however that turn began or ended. */
class LiveTurnTest {
    private fun item(seq: Int, kind: String, text: String? = null, role: String = "assistant", tool: String? = null,
        queued: Boolean? = null) =
        LineItem(Item("$seq#0", seq, role = role, kind = kind, text = text, toolUseId = tool,
            toolName = tool?.let { "Bash" }, queued = queued))
    private fun user(seq: Int, text: String, queued: Boolean? = null) = item(seq, "text", text, "user", queued = queued)
    private fun tool(seq: Int) = item(seq, "tool_use", tool = "t$seq")
    private fun live(lines: List<ChatLine>) = lines.filterIsInstance<ChatLine.Work>().filter { it.live }

    /** A message sent while it works joins the running turn, whose id stays its first message's. */
    @Test fun aQueuedMessageKeepsTheTurnLive() {
        val items = listOf(user(1, "跑一下测试"), tool(2), user(3, "顺便看下 lint", queued = true), tool(4))
        val snapshot = chatLines(items, LineOptions(live = true))
        assertEquals("1#0", snapshot.liveTurnId)
        val work = live(snapshot.lines).single()
        assertEquals("w-3#0", work.id)
        assertEquals(work, snapshot.lines.last())
    }

    /** A turn a background task's notice opened (after a recap here) has no user message at all. */
    @Test fun aTurnANoticeOpenedIsLive() {
        val items = listOf(user(1, "a"), item(2, "text", "b"), item(3, "recap", "总结"),
            item(4, "notice", "后台任务完成", "system"), tool(5))
        val snapshot = chatLines(items, LineOptions(live = true))
        assertEquals("4#0", snapshot.liveTurnId)
        val work = live(snapshot.lines).single()
        assertEquals(work, snapshot.lines.last())
        assertEquals(2, work.steps.size)
    }

    /** Working after a recap: no turn of its own yet, an empty live work line all the same. */
    @Test fun aRecapAtTheEndStillHasALiveLine() {
        val items = listOf(user(1, "a"), item(2, "text", "b"), item(3, "recap", "总结"))
        val snapshot = chatLines(items, LineOptions(live = true))
        val work = snapshot.lines.last() as ChatLine.Work
        assertEquals(LiveWorkId, work.id)
        assertTrue(work.live)
        assertTrue(work.steps.isEmpty())
    }

    @Test fun onlyTheLastTurnIsLiveAndNothingWhenIdle() {
        val items = listOf(user(1, "a"), tool(2), item(3, "text", "b"), user(4, "c"), tool(5))
        assertEquals(listOf("w-4#0"), live(chatLines(items, LineOptions(live = true)).lines).map { it.id })
        assertTrue(live(chatLines(items).lines).isEmpty())
        assertTrue(live(chatLines(items + item(6, "recap", "总结")).lines).isEmpty())
    }

    /** A frozen window keeps the turn it froze with and adds no line of its own. */
    @Test fun aFrozenWindowKeepsItsTurn() {
        val items = listOf(user(1, "a"), tool(2), item(3, "recap", "总结"))
        val frozen = chatLines(items, LineOptions(liveTurnId = "1#0", live = true)).lines
        assertEquals(listOf("w-1#0"), live(frozen).map { it.id })
    }

    @Test fun workingOrBlockedIsLive() {
        assertTrue(isLiveStatus("working"))
        assertTrue(isLiveStatus("blocked"))
        assertFalse(isLiveStatus("done"))
        assertFalse(isLiveStatus("idle"))
        assertFalse(isLiveStatus(null))
    }
}
