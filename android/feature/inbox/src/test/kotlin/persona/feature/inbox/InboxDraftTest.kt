package persona.feature.inbox

import persona.core.domain.inbox.InboxItem
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

/** AI 草拟 / AI 改 and the confirmation an AI draft needs before it goes out in the user's name. */
@OptIn(ExperimentalCoroutinesApi::class)
class InboxDraftTest {
    @Test fun instructionIsTheRequestThenTheDraftToRevise() {
        assertEquals("", draftInstruction("  ", ""))
        assertEquals("语气轻松一点", draftInstruction(" 语气轻松一点 ", ""))
        assertEquals("在这版上改：能赶上", draftInstruction("", "能赶上\n"))
        assertEquals("短一点\n在这版上改：能赶上，周五前给你。", draftInstruction("短一点", "能赶上，周五前给你。"))
    }

    @Test fun anAiDraftIsMarkedAndKeepsItsMarkWhileEdited() = runTest {
        val repository = FakeInboxRepository()
        val composer = ReplyComposer(repository, ReplyTarget.Inbox("item"), backgroundScope)
        runCurrent()
        composer.update("我自己写的")
        runCurrent()
        assertFalse(composer.state.value.draft.ai)
        val typed = composer.state.value.draft.clientId
        assertTrue(composer.replaceWithAi("AI 写的回复"))
        runCurrent()
        val ai = composer.state.value.draft
        assertTrue(ai.ai)
        assertEquals("AI 写的回复", ai.text)
        assertNotEquals(typed, ai.clientId)
        assertTrue(repository.draft!!.ai)
        composer.update("AI 写的回复，改了两个字")
        assertTrue(composer.state.value.draft.ai)
        // Clearing it all makes the reply the user's own again.
        composer.update("")
        assertFalse(composer.state.value.draft.ai)
        composer.update("重新写")
        assertFalse(composer.state.value.draft.ai)
    }

    @Test fun aSendOnItsWayKeepsItsTextAndId() = runTest {
        val repository = FakeInboxRepository()
        val composer = ReplyComposer(repository, ReplyTarget.Inbox("item"), backgroundScope)
        runCurrent()
        composer.replaceWithAi("第一版")
        runCurrent()
        composer.send() // the fake's first reply fails, so its result is unclear
        runCurrent()
        assertEquals(ReplyDelivery.Unknown, composer.state.value.delivery)
        assertFalse(composer.replaceWithAi("第二版"))
        assertEquals("第一版", composer.state.value.draft.text)
        assertEquals(repository.requests.single().clientId, composer.state.value.draft.clientId)
    }

    @Test fun theAgentsDraftFillsOnlyAnUntouchedReply() = runTest {
        val repository = FakeInboxRepository()
        val target = ReplyTarget.Inbox("item")
        // A page opened before the list: the reply starts empty, then the AI 分身's draft comes.
        val late = ReplyComposer(repository, target, backgroundScope)
        late.offerAiDraft("AI 分身的草稿") // while the saved draft is still being read
        runCurrent()
        assertEquals("AI 分身的草稿", late.state.value.draft.text)
        assertTrue(late.state.value.draft.ai)

        // The user's own text is never replaced.
        val typed = ReplyComposer(FakeInboxRepository(), target, backgroundScope)
        runCurrent()
        typed.update("我先写了")
        typed.offerAiDraft("AI 分身的草稿")
        assertEquals("我先写了", typed.state.value.draft.text)

        // Nor is a saved draft, even an empty one the user cleared.
        val saved = FakeInboxRepository().apply { draft = ReplyDraft("") }
        val cleared = ReplyComposer(saved, target, backgroundScope)
        runCurrent()
        cleared.offerAiDraft("AI 分身的草稿")
        assertEquals("", cleared.state.value.draft.text)

        // An AI 分身 draft given up front is an AI draft from the start.
        val upFront = ReplyComposer(FakeInboxRepository(), target, backgroundScope, "AI 分身的草稿")
        runCurrent()
        assertTrue(upFront.state.value.draft.ai)
        val restored = FakeInboxRepository().apply { draft = ReplyDraft("存过的 AI 草稿", ai = true) }
        val reopened = ReplyComposer(restored, target, backgroundScope)
        runCurrent()
        assertTrue(reopened.state.value.draft.ai)
    }

    @Test fun confirmationSaysWhereAndEveryWord() {
        val lark = InboxRecord(InboxItem("l", title = "设计评审"), feedId = "f2", source = "lark")
        val confirmation = sendConfirmation(lark, "  好的，下午看。\n")
        assertEquals("以你的身份发送？", confirmation.title)
        assertEquals("发到飞书「设计评审」：\n\n好的，下午看。", confirmation.message)
        val mail = InboxRecord(InboxItem("m", kind = "mail", title = "周报"), source = "mail")
        assertEquals("回复邮件「周报」", sendConfirmation(mail, "收到").destination)
        val reply = mailSendConfirmation("", "示例联系人 <contact@example.com>", "收到", targetChanged = true)
        assertEquals("回复邮件「无主题」，发给 示例联系人 <contact@example.com>", reply.destination)
        assertNotNull(reply.warning)
        assertNull(mailSendConfirmation("周报", "", "收到", targetChanged = false).warning)
    }
}
