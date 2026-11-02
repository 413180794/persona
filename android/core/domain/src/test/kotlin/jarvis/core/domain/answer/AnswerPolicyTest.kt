package jarvis.core.domain.answer

import jarvis.core.contracts.DialogOption
import jarvis.core.contracts.Interaction
import jarvis.core.contracts.Question
import jarvis.core.contracts.QuestionOption
import jarvis.core.contracts.ScreenDialog
import org.junit.Assert.*
import org.junit.Test

class AnswerPolicyTest {
    @Test fun entryAndFormGatesApplyTogether() {
        val plan = Interaction("request", kind = "plan")
        for (entry in AnswerEntry.entries) {
            val unlocked = AnswerContext(entry = entry, fullPlanRead = true)
            val all = AnswerBuilder.build(plan, AnswerAction.ApproveAll, context = unlocked)
            assertEquals(entry.name, entry == AnswerEntry.Conversation, all.allowed)
            val edits = AnswerBuilder.build(plan, AnswerAction.ApproveEdits, context = unlocked)
            assertEquals("acceptEdits", edits.resolution?.mode)
            assertFalse(AnswerBuilder.build(plan, AnswerAction.ApproveEdits, context = unlocked.copy(locked = true)).allowed)
            assertFalse(AnswerBuilder.build(plan, AnswerAction.ApproveEdits,
                AnswerDraft(feedback = "先补测试"), unlocked).allowed)
        }
        val lockedNotification = AnswerContext(entry = AnswerEntry.Notification, locked = true)
        assertTrue(AnswerBuilder.build(plan, AnswerAction.Reject,
            AnswerDraft(feedback = "先补测试"), lockedNotification).allowed)
        val permission = Interaction("request", kind = "permission")
        val denial = AnswerBuilder.build(permission, AnswerAction.Deny,
            AnswerDraft(message = "请检查"), lockedNotification)
        assertEquals("请检查", denial.resolution?.message)
        val question = Interaction("request", kind = "question", questions = listOf(
            Question("选择？", options = (1..4).map { QuestionOption("选项 $it") })))
        assertFalse(AnswerBuilder.build(question, AnswerAction.PickOption, context = lockedNotification).allowed)
        val notification = lockedNotification.copy(locked = false)
        assertTrue(AnswerBuilder.build(question, AnswerAction.PickOption, context = notification).allowed)
        assertFalse(AnswerBuilder.build(question, AnswerAction.PickOption, AnswerDraft(optionIndex = 3), notification).allowed)
        val questions = requireNotNull(question.questions)
        assertFalse(AnswerBuilder.build(question.copy(questions = questions + questions),
            AnswerAction.PickOption, context = notification).allowed)
        assertFalse(AnswerBuilder.build(question.copy(questions = listOf(questions.single().copy(multiSelect = true))),
            AnswerAction.PickOption, context = notification).allowed)
        val screen = Interaction("request", kind = "screen", dialog = ScreenDialog(options = listOf(
            DialogOption("1", "确认"), DialogOption("2", "Type something.", kind = "type_something"))))
        assertFalse(AnswerBuilder.build(screen, AnswerAction.ScreenOption, AnswerDraft(option = "1"), lockedNotification).allowed)
        assertTrue(AnswerBuilder.build(screen, AnswerAction.ScreenOption, AnswerDraft(option = "1"), notification).allowed)
        assertFalse(AnswerBuilder.build(screen, AnswerAction.ScreenOption,
            AnswerDraft(option = "2", text = "继续"), notification).allowed)
        assertFalse(AnswerBuilder.build(question, AnswerAction.PickOption,
            context = AnswerContext(entry = AnswerEntry.Watch, locked = true)).allowed)
    }

    @Test fun multipleQuestionsWaitForSubmitAndKeepCustomMultiselectAnswer() {
        val first = Question("第一题？", options = listOf(QuestionOption("选项")))
        val last = first.copy(question = "第二题？")
        val interaction = Interaction("request", kind = "question", questions = listOf(first, last))
        val pickedFirst = AnswerBuilder.build(interaction, AnswerAction.PickOption)
        assertEquals(1, pickedFirst.nextPage)
        assertNull(pickedFirst.resolution)
        val pickedLast = AnswerBuilder.build(interaction, AnswerAction.PickOption, pickedFirst.draft)
        assertNull(pickedLast.resolution)
        val submitted = AnswerBuilder.build(interaction, AnswerAction.Submit, pickedLast.draft)
        assertEquals(mapOf(first.question to "选项", last.question to "选项"), submitted.resolution?.answers)
        val multi = interaction.copy(questions = listOf(first.copy(multiSelect = true)))
        val picked = AnswerBuilder.build(multi, AnswerAction.PickOption,
            AnswerDraft(other = mapOf(first.question to "  自填  ")))
        assertEquals("自填", AnswerBuilder.build(multi, AnswerAction.Submit, picked.draft).resolution?.answers?.get(first.question))
        assertFalse(AnswerTransport.begin(submitted.resolution!!, true, true, "unknown").allowed)
    }
}
