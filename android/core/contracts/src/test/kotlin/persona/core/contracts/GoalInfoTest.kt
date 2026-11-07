package persona.core.contracts

import kotlinx.serialization.encodeToString
import org.junit.Assert.*
import org.junit.Test

/** `Session.goal` as the hub sends it (docs/protocol/protocol.md, docs/integrations/agent-goal.md). */
class GoalInfoTest {
    @Test fun decodesTheGoalTheHubSends() {
        val session = JsonCodec.decodeFromString<Session>("""{"session_id":"s1","status":"working","goal":{"objective":"给天气 App 加主屏幕小组件\n并跑通 UI 测试",
            "status":"active","reason":"小组件还没有深色模式","tokens_used":12345,"token_budget":500000,"seconds":600,"updated_at":"2026-11-05T09:00:00Z"}}""")
        val goal = session.goal!!
        assertEquals("active", goal.status)
        assertTrue(goal.isActive)
        assertFalse(goal.isComplete)
        assertEquals("给天气 App 加主屏幕小组件", goal.headline)
        assertEquals("小组件还没有深色模式", goal.reason)
        assertEquals(12345L, goal.tokensUsed)
        assertEquals(500000L, goal.tokenBudget)
        assertEquals(600L, goal.seconds)
        assertEquals(session, JsonCodec.decodeFromString<Session>(JsonCodec.encodeToString(session)))
    }

    @Test fun noGoalAndOlderHubsLeaveItNull() {
        assertNull(JsonCodec.decodeFromString<Session>("""{"session_id":"s1","status":"idle"}""").goal)
    }

    @Test fun aMalformedGoalFieldDoesNotDropTheSession() {
        val session = JsonCodec.decodeFromString<Session>("""{"session_id":"s1","title":"保留","status":"idle","goal":{"objective":"保留","status":"complete","tokens_used":"bad"}}""")
        assertEquals("保留", session.title)
        assertEquals("保留", session.goal?.objective)
        assertTrue(session.goal!!.isComplete)
        assertNull(session.goal?.tokensUsed)
    }

    @Test fun theHeadlineIsTheFirstLineWithText() {
        assertEquals("跑通测试", GoalInfo(objective = "\n  跑通测试  \n再发版").headline)
        assertEquals("", GoalInfo(objective = "  ").headline)
    }
}
