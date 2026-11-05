package persona.core.domain.sort

import persona.core.domain.zhAny

import persona.core.contracts.Session
import persona.core.contracts.WorkAgent
import persona.core.contracts.WorkState
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class WorkLineTest {
    private val now = Instant.parse("2026-11-05T10:00:00Z")
    private fun at(minutes: Long) = now.plusSeconds(minutes * 60).toString()
    private fun session(work: WorkState?, state: String = "idle", status: String = "done") =
        Session(machineId = "m", sessionId = "s", status = status, state = state, work = work)

    @Test fun doneSaysTheConclusion() {
        assertEquals("✓ 已合并 PR #12", (session(WorkState("done", line = "已合并 PR #12")).workLine(now)).zhAny())
    }

    @Test fun blockedSaysWhatItWaitsForAndWhenTheHubActs() {
        assertEquals("等你选 D2 / D4", (session(WorkState("blocked", on = "user", line = "等你选 D2 / D4")).workLine(now)).zhAny())
        assertEquals("等部署跑完（12 分钟后叫醒）",
            (session(WorkState("blocked", on = "external", line = "等部署跑完", wait = "部署", wakeAt = at(12))).workLine(now)).zhAny())
        assertEquals("模型服务暂时出错（马上重试）",
            (session(WorkState("blocked", on = "transient", line = "模型服务暂时出错", wakeAt = now.plusSeconds(20).toString())).workLine(now)).zhAny())
        assertEquals("等（1 小时 5 分后叫醒）", (session(WorkState("blocked", on = "external", line = "等", wakeAt = at(65))).workLine(now)).zhAny())
    }

    @Test fun nothingWhileItWorksOrBeforeTheHubTriagedIt() {
        assertNull(session(WorkState("working"), state = "working", status = "working").workLine(now))
        assertNull(session(null).workLine(now))
        assertNull(session(WorkState("done")).workLine(now))
    }

    @Test fun theHubWaitsOutWhatEndsWithoutTheUser() {
        assertTrue(WorkState("blocked", on = "external", wait = "部署", wakeAt = at(5)).hubWaits)
        assertTrue(WorkState("blocked", on = "external", line = "后台 2 个任务在跑").hubWaits)
        assertFalse(WorkState("blocked", on = "external", wait = "部署").hubWaits) // nobody will wake it
        assertFalse(WorkState("blocked", on = "agent", wait = "它", agent = WorkAgent("m", "o")).hubWaits)
        assertFalse(WorkState("blocked", on = "user", wakeAt = at(5)).hubWaits)
        assertFalse(WorkState("done").hubWaits)
    }

    @Test fun aSessionTheHubWaitsOutIsWaiting() {
        val waiting = AgentSession(session(WorkState("blocked", on = "external", wait = "部署", wakeAt = at(5), line = "等部署")))
        assertTrue(waiting.isWaiting)
        assertFalse(AgentSession(session(WorkState("done", line = "做完了"))).isWaiting)
    }
}
