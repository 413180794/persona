package jarvis.android

import jarvis.android.data.Interaction
import jarvis.android.data.JsonCodec
import jarvis.android.data.LoopInfo
import jarvis.android.data.Machine
import jarvis.android.data.Pins
import jarvis.android.data.Question
import jarvis.android.data.Session
import jarvis.android.data.SessionKey
import jarvis.android.data.pinKey
import jarvis.android.data.spacePinKey
import jarvis.android.fleet.FleetItem
import jarvis.android.fleet.FleetState
import jarvis.android.fleet.FleetText
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.Instant

class FleetTest {
    private val t0 = Instant.parse("2026-10-29T08:00:00Z")
    private val machines = listOf(Machine("mac", name = "MacBook"), Machine("srv", name = "服务器"))

    private fun s(id: String, state: String, machine: String = "mac", title: String = "", minutes: Long = 0,
                  space: String? = null, slot: String? = null, assistant: Boolean = false, last: String? = null) =
        Session(
            machineId = machine, sessionId = id, title = title, cwd = "/home/me/$id", state = state,
            updatedAt = t0.plusSeconds(minutes * 60).toString(), spaceName = space, slotName = slot, slotIndex = 1,
            assistant = assistant, lastText = last,
        )

    private fun muted(p: Pins): (Session) -> Boolean = { p.mutes(it.pinKey(), it.spacePinKey()) }

    @Test fun buildMatchesTheHubRules() {
        val sessions = listOf(
            s("w1", "working", minutes = 5, title = "Claude Code", space = "weather", slot = "api"),
            s("w2", "working", minutes = 1, title = "迁移数据库"),
            s("u1", "unread", minutes = 2, last = "全部测试通过了。\n细节……"),
            s("u2", "unread", minutes = 9),
            s("idle", "idle"),
            s("q1", "working", machine = "srv", title = "问题"),
            s("tui", "needs_you", title = "终端"),
            s("muted", "working", title = "静音的"),
            s("asst", "working", machine = "srv", assistant = true),
            s("loop", "unread").copy(loop = LoopInfo(kind = "loop")),
        )
        val ixs = listOf(
            Interaction("ix2", "srv", "q1", kind = "question", createdAt = t0.plusSeconds(30).toString(),
                questions = listOf(Question(question = "用哪个   数据库？"))),
            Interaction("ix1", "mac", "w2", kind = "permission", createdAt = t0.toString(), summary = "Bash: go test ./..."),
            Interaction("hub", "", "", kind = "permission", createdAt = t0.toString()), // no session here: left out
        )
        val st = FleetState.build(sessions, ixs, machines, muted(Pins(sessions = listOf("mac/muted"))))

        assertEquals("needsYou", st.phase)
        assertEquals(3, st.needsYou) // two interactions + the TUI dialog
        assertEquals(1, st.working) // w1 (w2 and q1 asked; muted and the assistant left out)
        assertEquals(2, st.unread) // the loop's left out
        assertTrue(st.assistant)
        assertEquals(FleetState.MAX_ITEMS, st.items.size)
        // Oldest request first, with its detail.
        assertEquals("mac/w2", st.items[0].id)
        assertEquals("permission", st.items[0].kind)
        assertEquals("Bash: go test ./...", st.items[0].detail)
        assertEquals("ix1", st.items[0].interaction)
        assertEquals("用哪个 数据库？", st.items[1].detail)
        assertEquals("服务器", st.items[1].place)
        assertEquals("screen", st.items[2].kind)
        assertEquals(st.items[0], st.firstActionable)
        assertEquals(4, st.hidden(2)) // 6 agents, 2 drawn ⇒ 「+4 个」
        assertNull(st.single)
        // A plan's first line is usually a heading: its marks are not part of the detail.
        val plan = Interaction("p", "mac", "w1", kind = "plan", createdAt = t0.toString(), plan = "\n## 支持 HEIC\n1. 改")
        assertEquals("支持 HEIC", FleetState.build(sessions, listOf(plan), machines) { false }.items[0].detail)
    }

    @Test fun mutedSpacesAndTheAgentsTurnedBackOn() {
        val sessions = listOf(s("a", "working", space = "quiet"), s("b", "working", space = "quiet"))
        val p = Pins(spaces = listOf("mac/quiet"), allowed = listOf("mac/b"))
        val st = FleetState.build(sessions, emptyList(), machines, muted(p))
        assertEquals(listOf("mac/b"), st.items.map { it.id })
        assertEquals(SessionKey("mac", "b"), st.single)
    }

    @Test fun workingAndDoneOrdering() {
        val sessions = listOf(
            s("w1", "working", minutes = 5, title = "Claude Code", space = "weather", slot = "api"),
            s("w2", "working", minutes = 1),
            s("u1", "unread", minutes = 2, last = "\n全部测试通过了。\n细节……"),
        )
        val st = FleetState.build(sessions, emptyList(), machines) { false }
        assertEquals("working", st.phase)
        // The longest-running turn first; its start runs the clock.
        assertEquals(listOf("mac/w2", "mac/w1", "mac/u1"), st.items.map { it.id })
        assertEquals(t0.plusSeconds(60).epochSecond.toDouble(), st.workingSince!!, 0.0)
        assertEquals("w1", st.items[1].title) // "Claude Code" falls back to the folder
        assertEquals("MacBook · weather › api", st.items[1].place)
        assertEquals("全部测试通过了。", st.items[2].detail)
        assertNull(st.firstActionable)

        assertEquals("done", FleetState.build(listOf(s("u", "unread")), emptyList(), machines) { false }.phase)
        assertTrue(FleetState.build(listOf(s("i", "idle")), emptyList(), machines) { false }.isEmpty)
    }

    @Test fun wording() {
        val st = FleetState(phase = "needsYou", needsYou = 2, working = 3, unread = 1)
        assertEquals("2 个需要你", FleetText.headline(st))
        assertEquals("3 个在工作 · 1 个做完了", FleetText.rest(st))
        assertEquals("都处理完了", FleetText.headline(FleetState()))
        assertEquals("授权：Bash: ls", FleetText.line(FleetItem("a/b", kind = "permission", detail = "Bash: ls")))
        assertEquals("计划等你批准", FleetText.line(FleetItem("a/b", kind = "plan")))
        assertEquals("有个授权等你", FleetText.anonymous(FleetItem("a/b", kind = "permission")))
        assertEquals(SessionKey("m", "s/x"), FleetItem("m/s/x").key)
        assertNull(FleetItem("nokey").key)
        assertEquals("abc…", FleetState.clip("abcdef", 4))
    }

    @Test fun snapshotRoundTripsAndReadsTheHubsShape() {
        val st = FleetState("working", working = 1, items = listOf(FleetItem("m/s", "t", "机器", "working", since = 1.0)), workingSince = 1.0)
        assertEquals(st, JsonCodec.decodeFromString(FleetState.serializer(), JsonCodec.encodeToString(FleetState.serializer(), st)))
        // The hub's own FleetState JSON (liveactivity.go) decodes too, extra fields ignored.
        val hub = """{"phase":"working","needsYou":0,"working":1,"unread":0,
            "items":[{"id":"m/s","title":"t","place":"机器 · 空间","kind":"working","detail":"","since":1761700000}],
            "workingSince":1761700000,"details":false,"stats":{"tpm":12000}}"""
        val decoded = JsonCodec.decodeFromString(FleetState.serializer(), hub)
        assertEquals("机器 · 空间", decoded.items[0].place)
        assertEquals(1761700000.0, decoded.workingSince!!, 0.0)
    }
}
