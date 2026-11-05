package persona.feature.agents

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.*

class TodosLogicTest {
    private fun session(machine: String, id: String, state: String, lastText: String? = null) =
        Session(
            machineId = machine,
            sessionId = id,
            state = state,
            lastText = lastText
        )

    @Test
    fun mostUrgentAgentSelectsNeedsYouOverWorkingAndUnread() {
        val sNeeds = session("mac", "s_needs", "needs_you", lastText = "请问选 A 还是 B？")
        val sWorking = session("mac", "s_working", "working", lastText = "正在跑构建")
        val sUnread = session("mac", "s_unread", "unread", lastText = "已经完成构建")
        val sessions = listOf(sNeeds, sWorking, sUnread).associateBy { it.key }
        val lookup: (SessionKey) -> Session? = { sessions[it] }

        val todo = Todo(
            id = "t1",
            title = "紧急任务",
            agents = listOf(
                TodoAgent("mac", "s_working"),
                TodoAgent("mac", "s_unread"),
                TodoAgent("mac", "s_needs")
            )
        )

        val urgent = mostUrgentAgent(todo, lookup)
        assertNotNull(urgent)
        assertEquals("s_needs", urgent?.agent?.sessionId)
        assertEquals(AgentState.NeedsYou, urgent?.status)
        assertEquals("请问选 A 还是 B？", urgent?.session?.lastText)
    }

    @Test
    fun mostUrgentAgentHandlesFailedAgent() {
        val todo = Todo(
            id = "t_fail",
            title = "启动失败任务",
            agents = listOf(
                TodoAgent("mac", "s_fail", startError = "node crashed")
            )
        )
        val urgent = mostUrgentAgent(todo) { null }
        assertNotNull(urgent)
        assertTrue(urgent?.isFailed == true)
        assertEquals("node crashed", urgent?.agent?.startError)
    }

    @Test
    fun mostUrgentAgentReturnsNullWhenNoAgents() {
        val todo = Todo(id = "empty", title = "空任务", agents = emptyList())
        assertNull(mostUrgentAgent(todo) { null })
    }

    @Test
    fun contextXmlAndHandOffPromptFormatting() {
        val xml = todoContextXml("优化冷启动", "说明内容")
        assertTrue(xml.startsWith("<jarvis-context kind=\"context\" title=\"待办 · 优化冷启动\" url=\"\">"))
        assertTrue(xml.contains("说明内容"))
        assertTrue(xml.endsWith("</jarvis-context>"))

        val promptWithInstruction = todoHandOffPrompt("优化冷启动", "说明内容", "先查 Profiler trace")
        assertTrue(promptWithInstruction.contains(xml))
        assertTrue(promptWithInstruction.endsWith("\n\n先查 Profiler trace"))

        val promptEmptyInstruction = todoHandOffPrompt("优化冷启动", "说明内容", "   ")
        assertEquals(xml, promptEmptyInstruction)
    }

    @Test
    fun mostUrgentSummaryFollowsIosOrder() {
        val agent = TodoAgent("mac", "s1")
        fun summary(session: Session?, failed: Boolean = false, error: String? = null, title: String? = null) =
            mostUrgentSummary(TodoMostUrgent(agent.copy(startError = error, title = title), session, AgentState.Working, failed))
        assertNull(mostUrgentSummary(null))
        assertEquals(TodoUrgentSummary.StartFailed("no claude"), summary(null, failed = true, error = " no claude "))
        // A failed start with no error falls through, to nothing at all: the notes show instead.
        assertNull(summary(null, failed = true, error = ""))
        assertEquals(TodoUrgentSummary.Words("Fix the build now"), summary(session("mac", "s1", "working", lastText = "## Fix **the** `build` [now](https://example.com)")))
        val quiet = Session(machineId = "mac", sessionId = "s1", state = "working", title = "修登录页", lastText = "  ")
        assertEquals(TodoUrgentSummary.Words(quiet.displayTitle), summary(quiet, title = "agent title"))
        assertEquals(TodoUrgentSummary.Words("agent title"), summary(null, title = "agent title"))
        assertNull(summary(null, title = " "))
    }
}
