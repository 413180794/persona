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
