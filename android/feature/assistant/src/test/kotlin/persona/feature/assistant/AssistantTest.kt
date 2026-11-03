package persona.feature.assistant

import persona.core.contracts.Session
import persona.core.contracts.api.AssistantSnapshot
import java.time.Instant
import java.time.ZoneId
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class AssistantTest {
    @Test fun searchSeparatesPinnedAndRecentAndIncludesMainOnlyOnce() {
        val main = Session(machineId = "demo", sessionId = "main", title = "主对话")
        val pinned = Session(machineId = "demo", sessionId = "pinned", title = "周报素材")
        val recent = Session(machineId = "demo", sessionId = "recent", title = "工作安排", lastText = "周报")
        val result = conversationGroups(AssistantSnapshot(main.key, listOf(main, pinned, recent, recent)), " 周报 ", setOf(pinned.key))
        assertEquals(listOf(pinned), result.pinned)
        assertEquals(listOf(recent), result.recent)
        val mainOnly = conversationGroups(AssistantSnapshot(main.key, listOf(main, main)), "主对话", setOf(main.key))
        assertEquals(main, mainOnly.main)
        assertTrue(mainOnly.recent.isEmpty())
        assertTrue(mainOnly.pinned.isEmpty())
        assertNull(conversationGroups(AssistantSnapshot(main.key, listOf(main)), "周报", emptySet()).main)
    }

    @Test fun historyFormatting() {
        val session = Session(machineId = "demo", sessionId = "history", title = "Claude Code",
            status = "offline", lastText = "\n继续整理安排")
        assertEquals("继续整理安排", session.conversationTitle())
        assertEquals(persona.core.designsystem.Status.Offline, session.conversationStatus())
        assertEquals("10月31日 17:30", conversationTime(Instant.parse("2026-10-31T09:30:00Z"), ZoneId.of("Asia/Shanghai")))
        assertNull(conversationTime(null))
    }

    @Test fun handoffConversationTitle() {
        // From the old app's InboxTest: the hub names a hand-off after its first message, a <jarvis-context> tag.
        val s = Session(machineId = "m", sessionId = "x", title = "<jarvis-context kind=\"context\" title=\"飞书 · 发布群\">(feed_id=f2)")
        assertEquals("交给 Persona：飞书 · 发布群", s.conversationTitle())
        val escaped = Session(machineId = "m", sessionId = "z", title = "<jarvis-context kind=\"context\" title=\"飞书 · a&quot;b &lt;c&gt; &amp;amp;\" url=\"\">")
        assertEquals("交给 Persona：飞书 · a\"b <c> &amp;", escaped.conversationTitle())
        assertEquals("早报", Session(machineId = "m", sessionId = "y", title = "早报").conversationTitle())
        // No title on the tag, or an empty one: still a hand-off, not the raw tag.
        assertEquals("交给 Persona", Session(machineId = "m", sessionId = "a", title = "<jarvis-context kind=\"context\">").conversationTitle())
        assertEquals("交给 Persona", Session(machineId = "m", sessionId = "b", title = "<jarvis-context title=\"\" url=\"\">").conversationTitle())
        // Untitled: the last message's first line, read before it is cut to 30 characters.
        val untitled = Session(machineId = "m", sessionId = "c", title = "Claude Code",
            lastText = "\n<jarvis-context kind=\"context\" title=\"项目 · 一个名字很长很长很长很长很长很长的项目\" url=\"\">\n正文")
        assertEquals("交给 Persona：项目 · 一个名字很长很长很长很长很长很长的项目", untitled.conversationTitle())
        assertEquals("a".repeat(30), Session(machineId = "m", sessionId = "d", lastText = "a".repeat(40)).conversationTitle())
        assertNull(handoffTitle("早报 <jarvis-context"))
    }

    @Test fun theRecentGroupShowsThirtyThenTheRest() {
        val recent = (1..45).map { Session(machineId = "m", sessionId = "s$it", title = "对话 $it") }
        val folded = recentRows(recent, query = "", showAll = false)
        assertEquals(recent.take(30), folded.shown)
        assertEquals(15, folded.hidden)
        assertEquals(recent, recentRows(recent, query = "", showAll = true).shown)
        assertEquals("a search lists everything it finds", 0, recentRows(recent, query = "对话", showAll = false).hidden)
        assertEquals(0, recentRows(recent.take(30), query = "", showAll = false).hidden)
    }

    @Test fun theHistoryRowMenu() {
        fun labels(pinned: Boolean, live: Boolean, pin: Boolean, end: Boolean) =
            conversationActions(pinned, live, if (pin) ({}) else null, {}, if (end) ({}) else null).map { it.label }
        assertEquals(listOf("置顶", "拷贝会话 ID", "结束对话"), labels(pinned = false, live = true, pin = true, end = true))
        assertEquals(listOf("取消置顶", "拷贝会话 ID"), labels(pinned = true, live = false, pin = true, end = true))
        assertEquals("the main conversation has no pin", listOf("拷贝会话 ID", "结束对话"), labels(pinned = false, live = true, pin = false, end = true))
        assertEquals(listOf("拷贝会话 ID"), labels(pinned = false, live = true, pin = false, end = false))
        assertTrue(conversationActions(false, true, null, {}, {}).last().destructive)
    }

    @Test fun firstConversationBecomesMainAndSendUsesOutbox() = runTest {
        val repository = DemoAssistantRepository()
        val main = repository.start()
        val branch = repository.start()
        assertEquals(main, repository.assistant.value.mainSession)
        assertNotEquals(main, branch)
        repository.send(main, "整理日程", "message", null)
        repository.send(main, "整理日程", "message", null)
        assertEquals(1, repository.outbox.value.size)
        assertEquals(main, repository.outbox.value.single().session)
        assertEquals("整理日程", repository.assistant.value.conversations.single { it.key == main }.lastText)
    }
}
