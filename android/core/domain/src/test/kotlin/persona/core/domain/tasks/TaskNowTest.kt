package persona.core.domain.tasks

import persona.core.domain.zhAny

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.Session
import persona.core.contracts.Subagent

/** A task card's progress (#1583), as iOS TaskNow and web taskNow. */
class TaskNowTest {
    private val base = Session(machineId = "mac-a", sessionId = "s1", status = "working", state = "working")

    @Test fun anAgentThatOnlyRanToolsShowsItsStep() {
        val s = base.copy(activity = "在跑测试", activityAt = "2026-11-04T11:50:00Z")
        assertEquals(TaskNow.Step("在跑测试", "2026-11-04T11:50:00Z"), TaskNow.of(s, working = true))
    }

    @Test fun aWorkingAgentsStepComesBeforeItsWordsAndTheWordsOnceItStopped() {
        val s = base.copy(lastText = "已建 issue 并认领", activity = "在读 tasks.go", activityAt = "2026-11-04T11:50:00Z")
        assertEquals(TaskNow.Step("在读 tasks.go", "2026-11-04T11:50:00Z"), TaskNow.of(s, working = true))
        assertEquals(TaskNow.Words("已建 issue 并认领"), TaskNow.of(s.copy(state = "unread"), working = false))
        // Stopped on a tool call, never said a thing: still the step, not timed.
        assertEquals(TaskNow.Step("在跑测试", null), TaskNow.of(base.copy(activity = "在跑测试", activityAt = "2026-11-04T11:50:00Z"), working = false))
    }

    @Test fun newWordsOrNothing() {
        assertEquals(TaskNow.Words("测试过了"), TaskNow.of(base.copy(lastText = " 测试过了 "), working = true))
        assertNull(TaskNow.of(base, working = true))
        assertNull(TaskNow.of(null, working = true))
    }

    @Test fun subagentsAddToTheMainAgentsProgress() {
        val s = base.copy(lastText = "两个只读子任务在调查", activity = "在搜 Shutdown", subagents = listOf(
            Subagent("a1", description = "Hub children lifecycle survey", running = true, lastText = "子任务的话"),
            Subagent("a2", type = "Explore", running = true),
            Subagent("a3", description = "做完的", running = false),
        ))
        assertEquals(TaskNow.Step("在搜 Shutdown", null), TaskNow.of(s, working = true))
        assertEquals(listOf("Hub children lifecycle survey", "Explore"), (TaskNow.subagents(s)).zhAny())
        assertEquals(emptyList<String>(), (TaskNow.subagents(base)).zhAny())
    }
}
