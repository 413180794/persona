package persona.feature.chat

import persona.core.contracts.*
import persona.core.contracts.api.*
import persona.core.domain.answer.*
import persona.core.domain.lines.ChatLine
import persona.core.domain.lines.WorkItem
import org.junit.Assert.*
import org.junit.Test

class ChatStateTest {
    @Test fun recordedDurationsStayWithHistoricalWorkAndMetadataAddsNoVisibleRow() {
        val user = Item("user#0", 1, role = "user", kind = "text")
        val thought = Item("thought#0", 2, role = "assistant", kind = "thinking", text = "检查示例")
        val answer = Item("reply#0", 3, role = "assistant", kind = "text")
        val marker = Item("duration#0", 4, role = "system", kind = "turn_duration",
            turnDuration = TurnDuration(12345, "claude_turn_duration", parentUuid = "reply"))
        val next = user.copy(id = "next#0", seq = 5)
        val timeline = ChatTimeline()
        val plain = timeline.lines(listOf(user, thought, answer, next), next.id, emptySet())
        val withDuration = timeline.lines(listOf(user, thought, answer, marker, next), next.id, emptySet())
        assertEquals(plain.lines, withDuration.lines)
        val durations = mapOf(user.id to 12L)
        val work = withDuration.lines.filterIsInstance<ChatLine.Work>()
        assertEquals(12L, work.first().recordedDuration(durations))
        assertNull(work.last().recordedDuration(durations))
        assertEquals("用时 12 秒", recordedDurationLabel(12))
        assertEquals("用时 0 秒", recordedDurationLabel(0))
        assertNull(recordedDurationLabel(null))
        assertNull(recordedDurationLabel(-1))
    }

    @Test fun historicalSnapshotSurvivesTurnRegroupingAndFoldExpansion() {
        val timeline = ChatTimeline()
        val user = Item("user", 10, role = "user", kind = "text", text = "检查改动")
        val fold = Item("fold", 11, kind = "fold", fold = Fold(items = 2, tools = 1, throughSeq = 12, throughId = "result"))
        val answer = Item("answer", 13, role = "assistant", kind = "text", text = "已完成")
        val initial = listOf(user, fold, answer)
        val original = timeline.lines(initial, user.id, emptySet())
        timeline.freeze(initial, user.id)
        val newerUser = Item("new-user", 14, role = "user", kind = "text", text = "继续")
        val rewrittenAnswer = answer.copy(text = "内容已变化")
        val older = Item("older", 1, role = "user", kind = "text", text = "更早的记录")
        val updated = listOf(older, user, fold, rewrittenAnswer, newerUser)
        val frozen = timeline.lines(updated, newerUser.id, emptySet())
        assertTrue(frozen.lines.map { it.id }.containsAll(original.lines.map { it.id }))
        assertFalse(frozen.lines.any { it.holds(newerUser.id) })
        assertEquals("已完成", frozen.lines.filterIsInstance<ChatLine.Row>().first { it.row.id == answer.id }.row.item?.text)
        assertTrue(frozen.lines.any { it.holds(older.id) })
        val use = Item("call", 11, kind = "tool_use", toolUseId = "tool", toolName = "Read")
        val result = Item("result", 12, kind = "tool_result", toolUseId = "tool", isError = true)
        timeline.unfold(fold, listOf(use, result))
        val work = frozen.lines.filterIsInstance<ChatLine.Work>().last()
        val expanded = timeline.lines(updated, newerUser.id, setOf(work.id))
        assertEquals(1, expanded.lines.filterIsInstance<ChatLine.Work>().last().knownFailedTools)
        assertTrue(expanded.lines.filterIsInstance<ChatLine.Step>().any { it.step is WorkItem.Tool })
        assertFalse(expanded.lines.filterIsInstance<ChatLine.Step>().any { it.step is WorkItem.Folded })
        assertEquals(expanded.lines.size, expanded.lines.map { it.id }.distinct().size)
        timeline.follow()
        assertTrue(timeline.lines(updated, newerUser.id, emptySet()).lines.any { it.holds(newerUser.id) })
    }

    @Test fun statusPriorityAndApprovalRulesUseDomainPolicy() {
        val session = Session(sessionId = "session", status = "working")
        val key = SessionKey("machine", "session")
        val message = OutboxMessage("operation", key, "消息", DeliveryState.NeedsConfirmation)
        val permission = Interaction("permission", sessionId = "session", kind = "permission")
        val snapshot = ChatSnapshot(loading = false)
        assertEquals(StatusKind.Unconfirmed, chatStatus(snapshot, session, null, listOf(message), listOf(permission), false)?.kind)
        assertEquals(StatusKind.Submitting, chatStatus(snapshot, session, null, listOf(message), listOf(permission), true)?.kind)
        assertEquals(StatusKind.Ended, chatStatus(snapshot.copy(ended = true), session, null, listOf(message), listOf(permission), true)?.kind)
        assertFalse(AnswerBuilder.build(permission, AnswerAction.Allow, AnswerDraft(message = "先别执行")).allowed)
        assertEquals("先别执行", AnswerBuilder.build(permission, AnswerAction.Deny, AnswerDraft(message = "先别执行")).resolution?.message)
        val plan = Interaction("plan", kind = "plan")
        assertFalse(AnswerBuilder.build(plan, AnswerAction.ApproveEdits, AnswerDraft(feedback = "请修改")).allowed)
        assertFalse(AnswerBuilder.build(plan, AnswerAction.ApproveAll).allowed)
        val question = Interaction("question", kind = "question", questions = listOf(Question("选择", options = listOf(QuestionOption("选项（推荐）")))))
        assertEquals(mapOf("选择" to "选项（推荐）"), AnswerBuilder.build(question, AnswerAction.PickOption).resolution?.answers)
    }
}
