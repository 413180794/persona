package persona.core.domain.fleet

import persona.core.contracts.FleetItem
import persona.core.contracts.FleetState
import persona.core.contracts.FleetEvent
import persona.core.contracts.JsonCodec
import persona.core.contracts.ServerEvent
import persona.core.contracts.SessionKey
import org.junit.Assert.*
import org.junit.Test

class FleetSummaryTest {
    private val permission = FleetItem("mac/w2", "迁移数据库", "MacBook · weather › api", "permission", "Bash: go test ./...", "ix1", 100.0)
    private val question = FleetItem("srv/q1", "问题", "服务器", "question", "用哪个数据库？", "ix2", 130.0)
    private val screen = FleetItem("mac/tui", "终端", "MacBook", "screen", "终端等你处理")

    @Test fun wordingMatchesTheIosApp() {
        val st = FleetState("needsYou", needsYou = 2, working = 3, unread = 1)
        assertEquals("2 个需要你", FleetText.headline(st))
        assertEquals("3 个在工作 · 1 个做完了", FleetText.rest(st))
        assertEquals("都处理完了", FleetText.headline(FleetState()))
        assertEquals("", FleetText.rest(FleetState("working", working = 2)))
        assertEquals("1 个做完了", FleetText.rest(FleetState("working", working = 2, unread = 1)))
        assertEquals("4 个做完了", FleetText.headline(FleetState("done", unread = 4)))
        assertEquals("授权：Bash: ls", FleetText.line(FleetItem("a/b", kind = "permission", detail = "Bash: ls")))
        assertEquals("等你授权", FleetText.line(FleetItem("a/b", kind = "permission")))
        assertEquals("计划等你批准", FleetText.line(FleetItem("a/b", kind = "plan")))
        assertEquals("计划：支持 HEIC", FleetText.line(FleetItem("a/b", kind = "plan", detail = "支持 HEIC")))
        assertEquals("在问你", FleetText.line(FleetItem("a/b", kind = "question")))
        assertEquals("工作中", FleetText.line(FleetItem("a/b", kind = "working", detail = "ignored")))
        assertEquals("做完了", FleetText.line(FleetItem("a/b", kind = "unread")))
        assertEquals("全部测试通过了。", FleetText.line(FleetItem("a/b", kind = "unread", detail = "全部测试通过了。")))
        // A kind this client does not know is a request on a screen (iOS decodes it as screen).
        assertEquals("终端等你处理", FleetText.line(FleetItem("a/b", kind = "unknown")))
        assertEquals("有个授权等你", FleetText.anonymous(permission))
        assertEquals("有个计划等你批准", FleetText.anonymous(FleetItem("a/b", kind = "plan")))
        assertEquals("有个提问等你回答", FleetText.anonymous(question))
        assertEquals("有个终端等你处理", FleetText.anonymous(screen))
        assertEquals("迁移数据库 · 授权：Bash: go test ./...", FleetText.row(permission))
        assertEquals("迁移数据库 · 授权：Bash: go test ./...（MacBook · weather › api）", FleetText.rowWithPlace(permission))
        assertEquals("终端 · 终端等你处理（MacBook）", FleetText.rowWithPlace(screen))
        assertEquals("+4 个", FleetText.more(4))
        // Without lock screen details: kinds and counts, never a name.
        assertEquals(listOf("有个授权等你", "有个提问等你回答", "2 个终端等你处理"), FleetText.anonymousRequests(listOf(
            permission, question, screen, FleetItem("a/b", kind = "working"), FleetItem("c/d", kind = "unknown"))))
        assertEquals("3 个计划等你批准", FleetText.anonymousCount("plan", 3))
        assertTrue(FleetText.anonymousRequests(emptyList()).isEmpty())
    }

    @Test fun selectionRules() {
        val st = FleetState("needsYou", needsYou = 3, working = 2, unread = 1, items = listOf(question, permission, screen))
        assertEquals(permission, FleetSummary.firstActionable(st))
        assertNull(FleetSummary.firstActionable(st.copy(items = listOf(question, permission.copy(interaction = null)))))
        assertEquals(4, FleetSummary.hidden(st, 2)) // 6 agents, 2 drawn ⇒ 「+4 个」
        assertEquals(3, FleetSummary.hidden(st, 5))
        assertNull(FleetSummary.single(st))
        val one = FleetState("working", working = 1, items = listOf(FleetItem("m/s/x", kind = "working")))
        assertEquals(SessionKey("m", "s/x"), FleetSummary.single(one))
        assertNull(FleetSummary.key(FleetItem("nokey")))
        assertNull(FleetSummary.key(FleetItem("m/")))
        assertTrue(FleetSummary.isEmpty(FleetState()))
        // A phase from a newer hub falls back to what the counts say.
        assertEquals("working", FleetSummary.phase(FleetState("unknown", working = 1)))
        assertTrue(FleetSummary.needsYou(screen))
        assertFalse(FleetSummary.needsYou(FleetItem("a/b", kind = "working")))
    }

    @Test fun snapshotsRoundTrip() {
        // FleetFeed keeps the last fleet on disk for the widget and the tile. (Unix times before 2000 read as unset: D5.)
        val since = 1_761_700_000.0
        val st = FleetState("working", working = 1, items = listOf(FleetItem("m/s", "t", "机器", "working", since = since)), workingSince = since)
        assertEquals(st, JsonCodec.decodeFromString(FleetState.serializer(), JsonCodec.encodeToString(FleetState.serializer(), st)))
    }

    @Test fun readsTheHubsFleetEvent() {
        val event = ServerEvent.decode("""{"type":"fleet","fleet":{"phase":"working","needsYou":0,"working":1,
            "unread":0,"items":[{"id":"m/s","title":"t","place":"机器 · 空间","kind":"working","detail":"","since":1761700000}],
            "workingSince":1761700000,"details":false,"stats":{"tpm":12000}}}""")
        val fleet = (event as FleetEvent).fleet
        assertEquals("机器 · 空间", fleet.items.single().place)
        assertEquals(1761700000.0, fleet.workingSince!!, 0.0)
        assertEquals("1 个在工作", FleetText.headline(fleet))
    }
}
