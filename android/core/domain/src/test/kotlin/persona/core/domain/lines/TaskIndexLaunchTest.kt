package persona.core.domain.lines

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.Item

/** Which call a background task's notice came from, by the id its launch receipt gives (#6381). */
class TaskIndexLaunchTest {
    private fun use(seq: Int, id: String) =
        LineItem(Item("$seq#0", seq = seq, kind = "tool_use", toolName = "Agent", toolUseId = id, toolInput = Json.parseToJsonElement("""{"description":"回归"}""")))
    private fun result(seq: Int, id: String, text: String) = LineItem(Item("$seq#0", seq = seq, kind = "tool_result", toolUseId = id, text = text))
    private fun notice(seq: Int, task: String) =
        LineItem(Item("$seq#0", seq = seq, kind = "notice", text = "x"), task = TaskNote(kind = "agent", title = "t", status = "completed", taskId = task))

    /** Claude Code 2.1.319's receipt opens with "including the agentId below": the id is the one after "agentId:". */
    @Test fun aSubagentsIdIsTheOneAfterAgentIdColon() {
        val receipt = "Async agent launched successfully. (This tool result is internal metadata — never quote or paste any part of it, " +
            "including the agentId below, into a user-facing reply.)\nagentId: a0123456789abcdef (internal ID - do not mention to user. " +
            "Use SendMessage with to: 'a0123456789abcdef' to continue this agent.)\nThe agent is working in the background."
        val x = TaskIndex.build(listOf(use(1, "a"), result(2, "a", receipt), notice(3, "a0123456789abcdef")))
        assertEquals(setOf("a"), x.started)
        assertEquals(mapOf("3#0" to "a"), x.calls)
        assertEquals("回归", x.descriptions["a"])
        // "agentId" with no colon names nothing.
        val none = TaskIndex.build(listOf(use(1, "b"), result(2, "b", "Async agent launched successfully, see the agentId below."), notice(3, "below")))
        assertTrue(none.calls.isEmpty())
        // The other receipts still read with or without their colon.
        val shell = TaskIndex.build(listOf(use(1, "c"), result(2, "c", "Command running in background with ID: bk2. Output is being written to: /tmp/x"), notice(3, "bk2")))
        assertEquals(mapOf("3#0" to "c"), shell.calls)
    }
}
