package persona.core.domain.lines

import persona.core.domain.zhAny

import persona.core.contracts.CompactBoundary
import persona.core.contracts.Item
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class CompactionsTest {
    private fun item(seq: Int, kind: String, text: String? = null, role: String = "assistant", tool: String? = null,
        compact: CompactBoundary? = null) =
        LineItem(Item("$seq#0", seq, role = role, kind = kind, text = text, toolUseId = tool,
            toolName = tool?.let { "Bash" }, compact = compact))
    private fun notice(seq: Int, text: String, compact: CompactBoundary? = null) = item(seq, "notice", text, "system", compact = compact)
    private fun dividers(lines: List<ChatLine>) = lines.filterIsInstance<ChatLine.Row>().mapNotNull { it.row.item?.compaction }
    private fun kinds(lines: List<ChatLine>) = lines.map {
        when (it) {
            is ChatLine.Row -> if (it.row.item?.compaction != null) "divider" else it.row.type
            is ChatLine.Work -> "work"
            else -> it::class.simpleName.orEmpty()
        }
    }

    @Test fun theLabelLeavesOutWhatIsNotKnown() {
        assertEquals("以上内容已压缩 · 手动 · 压缩前 18.1 万 token", (Compaction("manual", 180_902).label).zhAny())
        assertEquals("以上内容已压缩 · 自动 · 压缩前 15 万 token", (Compaction("auto", 150_000).label).zhAny())
        assertEquals("以上内容已压缩 · 空闲时", (Compaction("idle").label).zhAny())
        assertEquals("以上内容已压缩 · 压缩前 9500 token", (Compaction(preTokens = 9_500).label).zhAny())
        assertEquals("以上内容已压缩", (Compaction("something-new", 0).label).zhAny())
        assertEquals("87.5 万", (tokenCount(874_639)).zhAny())
    }

    /** The phone's "Compact context" after an answer: one divider, the command and the summary folded into it. */
    @Test fun aManualCompactionAfterAnAnswerIsOneDivider() {
        val items = listOf(item(1, "text", "看一下", "user"), item(2, "text", "看完了"),
            notice(3, CompactCommandNotice), notice(4, CompactedNotice, CompactBoundary("manual", 180_902)),
            notice(5, ContinuedNotice))
        val lines = chatLines(items).lines
        assertEquals(listOf("user", "reply", "divider"), (kinds(lines)).zhAny())
        val mark = dividers(lines).single()
        assertEquals("以上内容已压缩 · 手动 · 压缩前 18.1 万 token", (mark.label).zhAny())
        assertEquals(listOf(CompactCommandNotice, CompactedNotice, ContinuedNotice), mark.notes)
        assertEquals("4#0", ((lines.last() as ChatLine.Row).row.id).zhAny())
    }

    /** Claude Code writes the /compact line after the summary: it joins the boundary all the same. */
    @Test fun theCommandAfterTheBoundaryJoinsIt() {
        val items = listOf(item(1, "text", "a", "user"), item(2, "text", "b"),
            notice(3, CompactedNotice, CompactBoundary("manual", 20_000)), notice(4, ContinuedNotice),
            notice(5, CompactCommandNotice), notice(6, "Compacted (ctrl+o to see full summary)"))
        val lines = chatLines(items).lines
        assertEquals(listOf("user", "reply", "divider"), (kinds(lines)).zhAny())
        assertEquals(4, dividers(lines).single().notes.size)
    }

    /** A compaction on its own (the first thing on a page): a divider, no work line to open. */
    @Test fun aLoneCompactionIsADivider() {
        val lines = chatLines(listOf(notice(1, CompactedNotice, CompactBoundary("auto")), notice(2, ContinuedNotice),
            item(3, "text", "继续", "user"), item(4, "text", "好"))).lines
        assertEquals(listOf("divider", "user", "reply"), (kinds(lines)).zhAny())
        assertEquals("以上内容已压缩 · 自动", (dividers(lines).single().label).zhAny())
    }

    /** The summary without its boundary (on an older page) stands in for it; Codex's bare notice is one too. */
    @Test fun aSummaryAloneOrABareNoticeIsADivider() {
        val alone = chatLines(listOf(notice(1, ContinuedNotice), item(2, "text", "继续", "user"))).lines
        assertEquals(listOf("divider", "user"), (kinds(alone)).zhAny())
        val codex = chatLines(listOf(item(1, "text", "a", "user"), item(2, "text", "b"), notice(3, CompactedNotice))).lines
        assertEquals(listOf("user", "reply", "divider"), (kinds(codex)).zhAny())
        assertNull(dividers(codex).single().trigger)
    }

    /** Compacted while idle: Claude Code's note makes the trigger "While idle". */
    @Test fun anIdleNoteMarksTheTrigger() {
        val lines = chatLines(listOf(item(1, "text", "a", "user"), item(2, "text", "b"),
            notice(3, CompactedNotice, CompactBoundary("manual")), notice(4, ContinuedNotice),
            notice(5, "Compacted while idle, before the prompt cache expired"))).lines
        assertEquals("idle", (dividers(lines).single().trigger).zhAny())
    }

    /** Compacted in the middle of work: it stays a step of the work line, one step for the boundary and its summary. */
    @Test fun aCompactionMidTurnStaysInItsWork() {
        val items = listOf(item(1, "text", "做", "user"), item(2, "tool_use", tool = "a"),
            notice(3, CompactedNotice, CompactBoundary("auto", 200_000)), notice(4, ContinuedNotice),
            item(5, "tool_use", tool = "b"), item(6, "text", "好了"))
        val lines = chatLines(items).lines
        assertEquals(listOf("user", "work", "reply"), (kinds(lines)).zhAny())
        val work = lines.filterIsInstance<ChatLine.Work>().single()
        assertEquals(3, work.steps.size)
        assertTrue((work.steps[1] as WorkItem.Thought).item.compaction != null)
    }

    /** A "Compact context" still waiting for its boundary (the compaction is running) stays the notice it is. */
    @Test fun aCommandWithoutABoundaryStaysANotice() {
        val items = listOf(item(1, "text", "a", "user"), item(2, "text", "b"), notice(3, CompactCommandNotice))
        val folded = foldCompactions(items)
        assertEquals(items, folded)
        assertTrue(dividers(chatLines(items).lines).isEmpty())
    }

    @Test fun nothingChangesWithoutACompaction() {
        val items = listOf(item(1, "text", "a", "user"), notice(2, "已中断"), item(3, "text", "b"))
        assertTrue(foldCompactions(items) === items)
    }
}
