package persona.core.domain.tasks

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.time.temporal.ChronoUnit
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.AgentState
import persona.core.contracts.FeedItem
import persona.core.contracts.Item
import persona.core.contracts.JsonCodec
import persona.core.contracts.Session
import persona.core.contracts.Todo
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.ChatRow
import persona.core.domain.lines.LineItem
import persona.core.domain.lines.ToolCall
import persona.core.domain.lines.WorkItem

/** The assistant's feed among the chat's lines (docs/unified-entry.md), as iOS ChatLines.mergeFeed and web displayLines. */
class AssistantLinesTest {
    private val now = Instant.now().truncatedTo(ChronoUnit.SECONDS)
    private fun ago(min: Long) = now.minus(min, ChronoUnit.MINUTES).toString()
    private fun user(id: String, min: Long) = ChatLine.Row(ChatRow("user", LineItem(Item(id, role = "user", kind = "text", ts = ago(min)))))
    private fun reply(id: String, min: Long) = ChatLine.Row(ChatRow("reply", LineItem(Item(id, role = "assistant", kind = "text", ts = ago(min)))))
    private fun feed(id: String, min: Long) = FeedItem(id, "c1", ago(min), "task", "t")

    @Test fun anItemGoesAtTheEndOfItsExchangeWithADayLinePerDay() {
        val lines = listOf(user("u1", 60 * 26), reply("a1", 60 * 26 - 1), user("u2", 30), reply("a2", 29))
        val out = AssistantLines.lines(lines, listOf(feed("early", 60 * 26 - 1), feed("mid", 20)), ZoneOffset.UTC).map { it.id }
        assertEquals(2, out.count { it.startsWith("day-") })
        assertEquals(listOf("u1", "a1", "feed-early", "u2", "a2", "feed-mid"), out.filterNot { it.startsWith("day-") })
    }

    @Test fun mergeKeepsOneCopyOldestFirst() {
        val a = feed("a", 5); val b = feed("b", 10)
        assertEquals(listOf("b", "a"), AssistantLines.merge(listOf(a), listOf(b, a)).map { it.id })
    }

    private fun call(name: String, result: String?, failed: Boolean = false) = ToolCall(
        LineItem(Item("use-$name", kind = "tool_use", toolName = name, ts = ago(5))),
        result?.let { LineItem(Item("res-$name", kind = "tool_result", text = it, isError = failed)) }, emptyList())

    @Test fun quickResultsComeOnlyFromThePhonesWritesAndFollowTheirWork() {
        val ok = call("mcp__jarvis__phone_reminders_create", """{"id":"R1","title":"打电话","due":"2026-11-07T10:00:00+08:00","list":"提醒事项"}""")
        val r = QuickResult.of(ok)
        assertNotNull(r); assertEquals(true, r!!.reminder); assertEquals("打电话", r.title); assertEquals("提醒事项", r.place)
        assertNull(QuickResult.of(call("mcp__jarvis__phone_reminders_list", """{"id":"x"}""")))
        assertNull(QuickResult.of(call("phone_calendar_create_event", "denied", failed = true)))
        assertNull(QuickResult.of(call("phone_reminders_create", null)))
        val work = ChatLine.Work("wt-1", listOf(WorkItem.Tool(ok)), live = false, start = now, end = now, open = false)
        val out = AssistantLines.lines(listOf(user("u1", 6), work, reply("a1", 4)), emptyList(), ZoneOffset.UTC).map { it.id }
        assertEquals(listOf("u1", "wt-1", "quick-use-mcp__jarvis__phone_reminders_create", "a1"), out.filterNot { it.startsWith("day-") })
    }

    @Test fun aTasksStateFollowsItsAgents() {
        val todo = Todo("t", "t")
        assertEquals(TaskState.NotStarted, TaskState.of(todo, emptyList()))
        assertEquals(TaskState.NeedsYou, TaskState.of(todo, listOf(AgentState.Working, AgentState.NeedsYou)))
        assertEquals(TaskState.Unread, TaskState.of(todo, listOf(AgentState.Working, AgentState.Unread)))
        assertEquals(TaskState.Working, TaskState.of(todo, listOf(AgentState.Working, AgentState.Idle)))
        assertEquals(TaskState.Followup, TaskState.of(todo, listOf(AgentState.Idle)))
        assertEquals(TaskState.Done, TaskState.of(todo.copy(doneAt = ago(1)), listOf(AgentState.NeedsYou)))
        // Its agent did not start (#1077): failed, not done, until 重试 reopens it without the error (#1396).
        val failed = todo.copy(doneAt = ago(1), startError = "claude did not become ready within 1m0s")
        assertEquals(TaskState.Failed, TaskState.of(failed, emptyList()))
        assertEquals("没开起来", TaskState.Failed.label)
        assertEquals(TaskState.NotStarted, TaskState.of(failed.copy(doneAt = null, startError = null), emptyList()))
    }

    @Test fun aTaskCutOffByTheModelServiceIsInterrupted() {
        // #1516: its agent's turn ended in an API error — not done; going on is in progress again.
        val todo = Todo("t", "t")
        assertEquals(TaskState.Interrupted, TaskState.of(todo, listOf(AgentState.Unread), interrupted = true))
        assertEquals(TaskState.NeedsYou, TaskState.of(todo, listOf(AgentState.NeedsYou, AgentState.Unread), interrupted = true))
        assertEquals(TaskState.Working, TaskState.of(todo, listOf(AgentState.Working)))
        assertEquals(TaskState.Unread, TaskState.of(todo, listOf(AgentState.Unread)))
        assertEquals("中断了", TaskState.Interrupted.label)
        val cut = "API Error: Connection lost mid-response. The response above may be incomplete."
        fun s(state: String, turnError: String?) = Session(machineId = "m", sessionId = "s", state = state, turnError = turnError)
        assertTrue(s("unread", cut).interrupted)
        assertTrue(s("idle", cut).interrupted)
        assertFalse(s("working", cut).interrupted)
        assertFalse(s("unread", null).interrupted)
        assertEquals(Interruption("连不上模型服务", true), Interruption.of(s("unread", cut)))
        assertEquals(Interruption("登录过期了，要在电脑上重新 /login", false), Interruption.of(s("unread", "API Error: 401 authentication_error")))
        assertNull(Interruption.of(s("working", cut)))
        fun receipt(body: String) = FeedItem("r", kind = "receipt", body = JsonCodec.parseToJsonElement(body))
        assertEquals("interrupted", receiptState(receipt("""{"state":"interrupted","summary":"模型服务出错，中断了"}""")))
        assertEquals("interrupted", receiptState(receipt("""{"state":"done","summary":"$cut"}""")))
        assertEquals("done", receiptState(receipt("""{"state":"done","summary":"修好了 API Error: Connection lost 时的崩溃"}""")))
        assertEquals("failed", receiptState(receipt("""{"state":"failed","summary":"没开起来：x"}""")))
    }

    @Test fun dayLabels() {
        val today = LocalDate.of(2026, 11, 3)
        assertEquals("今天 · 11月3日 周二", AssistantLines.dayLabel(today, today))
        assertEquals("昨天 · 11月2日 周一", AssistantLines.dayLabel(today.minusDays(1), today))
        assertEquals("2025年11月3日 周一", AssistantLines.dayLabel(LocalDate.of(2025, 11, 3), today))
    }
}
