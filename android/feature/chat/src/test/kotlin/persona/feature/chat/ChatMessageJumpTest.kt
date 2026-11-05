package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Item
import persona.core.domain.lines.LineOptions
import persona.core.domain.lines.chatLines
import persona.core.navigation.MessageJumps

class ChatMessageJumpTest {
    private val items = listOf(
        Item("user", 30, role = "user", kind = "text", text = "开始"),
        Item("call", 31, kind = "tool_use", toolUseId = "tool", toolName = "Read"),
        Item("result", 32, kind = "tool_result", toolUseId = "tool", text = "结果"),
        Item("reply", 33, role = "assistant", kind = "text", text = "完成"),
    )
    private fun lines(open: Set<String> = emptySet()) = chatLines(items.map(Item::asLineItem), LineOptions(openWork = open)).lines

    @Test fun aDrawnMessageIsScrolledTo() {
        val step = messageJumpStep(lines(), "reply", emptySet(), hasOlder = true, pages = 0)
        assertEquals(MessageJumpStep.Scroll(searchTarget(lines(), "reply")!!.rowId), step)
    }

    @Test fun aMessageInAClosedWorkLineOpensItFirst() {
        assertEquals(MessageJumpStep.Expand(setOf("other", "w-user")),
            messageJumpStep(lines(), "result", setOf("other"), hasOlder = false, pages = 0))
        val opened = messageJumpStep(lines(setOf("w-user")), "result", setOf("w-user"), hasOlder = false, pages = 0)
        assertTrue(opened is MessageJumpStep.Scroll)
        assertTrue(lines(setOf("w-user")).any { it.id == (opened as MessageJumpStep.Scroll).rowId })
    }

    @Test fun olderPagesUpToTheLimitThenGivesUp() {
        assertEquals(MessageJumpStep.LoadOlder, messageJumpStep(lines(), "older#1", emptySet(), hasOlder = true, pages = 0))
        assertEquals(MessageJumpStep.LoadOlder, messageJumpStep(lines(), "older#1", emptySet(), hasOlder = true, pages = MESSAGE_JUMP_PAGE_LIMIT - 1))
        assertEquals(MessageJumpStep.NotFound, messageJumpStep(lines(), "older#1", emptySet(), hasOlder = true, pages = MESSAGE_JUMP_PAGE_LIMIT))
        assertEquals(MessageJumpStep.NotFound, messageJumpStep(lines(), "older#1", emptySet(), hasOlder = false, pages = 0))
    }

    @Test fun aJumpIsTakenOnceAndOnlyByItsChat() {
        MessageJumps.request("m1", "s1", "item#1")
        assertNull(MessageJumps.take("m1", "s2"))
        assertEquals("item#1", MessageJumps.take("m1", "s1"))
        assertNull(MessageJumps.take("m1", "s1"))
        assertNull(MessageJumps.pending)
    }
}
