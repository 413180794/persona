package jarvis.feature.chat

import jarvis.core.contracts.Session
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.*
import jarvis.core.designsystem.PendingStatus
import jarvis.core.designsystem.StatusLineKind
import jarvis.core.designsystem.resolveStatusLine
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class ChatDeliveryStateTest {
    @Test fun deliveryActionsUseOriginalIdsAndPreserveDraftsAcrossRaces() = runTest {
        val sender = FakeSender()
        val actions = ChatDeliveryActions(sender)
        val composer = ChatComposerState(ChatComposerDraft("后来输入的草稿"))
        val imageMessage = "原消息\n\n<attached_images>\n/tmp/photo.jpg\n</attached_images>"
        sender.outbox.value = listOf(sender.message.copy(text = imageMessage))
        sender.gate = CompletableDeferred()
        val editing = async { actions.perform("message", DeliveryAction.Edit, composer::restoreMessage) }
        runCurrent()
        assertFalse(actions.perform("message", DeliveryAction.Cancel, composer::restoreMessage))
        assertEquals(setOf("message"), actions.busyIds)
        sender.gate!!.complete(Unit)
        assertTrue(editing.await())
        assertEquals("后来输入的草稿\n\n原消息", composer.value.text)
        assertEquals("/tmp/photo.jpg", composer.attachments.single().path)
        assertEquals(listOf("cancel:message"), sender.calls)

        sender.outbox.value = listOf(sender.message.copy(state = DeliveryState.Unknown))
        val before = composer.draft
        assertFalse(actions.perform("message", DeliveryAction.Retry, composer::restoreMessage))
        assertFalse(actions.perform("message", DeliveryAction.Cancel, composer::restoreMessage))
        assertTrue(actions.perform("message", DeliveryAction.ConfirmDelivered, composer::restoreMessage))
        assertEquals(before, composer.draft)
        assertEquals(DeliveryState.Sent, sender.outbox.value.single().state)

        sender.outbox.value = listOf(sender.message.copy(state = DeliveryState.Verifying))
        sender.accept = false
        assertFalse(actions.perform("message", DeliveryAction.ConfirmNotDelivered, composer::restoreMessage))
        assertEquals(before, composer.draft)
        sender.accept = true
        assertTrue(actions.perform("message", DeliveryAction.ConfirmNotDelivered, composer::restoreMessage))
        assertEquals(DeliveryState.Refused, sender.outbox.value.single().state)
        assertTrue(composer.value.text.endsWith("原消息"))
        assertTrue(actions.perform("message", DeliveryAction.Retry, composer::restoreMessage))
        assertEquals("retry:message", sender.calls.last())
        assertTrue(actions.busyIds.isEmpty())
    }

    @Test fun unknownStatesUseSharedVocabularyAndSessionPriority() {
        val session = Session(sessionId = "session", status = "working")
        val snapshot = ChatSnapshot(loading = false, turnDurationSeconds = 42)
        val message = FakeSender().message
        val expected = mapOf(
            DeliveryState.Queued to PendingStatus.Waiting,
            DeliveryState.WaitingForNetwork to PendingStatus.Waiting,
            DeliveryState.Sending to PendingStatus.Sending,
            DeliveryState.Unknown to PendingStatus.Unconfirmed,
            DeliveryState.Verifying to PendingStatus.Verifying,
            DeliveryState.NeedsConfirmation to PendingStatus.NeedsReview,
            DeliveryState.Refused to PendingStatus.Refused,
            DeliveryState.Rejected to PendingStatus.Refused,
            DeliveryState.Sent to null,
        )
        DeliveryState.entries.forEach { assertEquals(expected[it], it.pendingStatus) }
        listOf(DeliveryState.Unknown, DeliveryState.Verifying, DeliveryState.NeedsConfirmation).forEach { state ->
            val line = chatStatus(snapshot, session, null, listOf(message.copy(state = state)), emptyList(), false)
            assertEquals(StatusLineKind.Unconfirmed, line?.kind)
            assertEquals("上次操作结果待确认", line?.text)
        }
        val stopping = chatStatusState(snapshot.copy(stopping = true), session, null, emptyList(), emptyList(), false)
        assertEquals(StatusLineKind.Stopping, resolveStatusLine(stopping)?.kind)
        assertEquals("会话已结束", resolveStatusLine(stopping.copy(ended = true))?.text)
        assertEquals("已换成新会话", resolveStatusLine(stopping.copy(replaced = true))?.text)
        assertEquals(StatusLineKind.MachineOffline, resolveStatusLine(stopping.copy(machineOffline = true))?.kind)
        assertNull(chatStatus(snapshot, session.copy(status = "done"), null, emptyList(), emptyList(), false))
    }

    private class FakeSender : MessageSender {
        val message = OutboxMessage("message", SessionKey("machine", "session"), "原消息", DeliveryState.WaitingForNetwork)
        override val outbox = MutableStateFlow(listOf(message))
        val calls = mutableListOf<String>()
        var gate: CompletableDeferred<Unit>? = null
        var accept = true
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
            error("Actions must never create a new message")
        }
        override suspend fun cancel(clientMessageId: String): Boolean {
            calls += "cancel:$clientMessageId"
            gate?.await()
            if (accept) outbox.value = emptyList()
            return accept
        }
        override suspend fun retry(clientMessageId: String): Boolean {
            calls += "retry:$clientMessageId"
            return accept
        }
        override suspend fun confirm(clientMessageId: String, delivered: Boolean): Boolean {
            calls += "confirm:$clientMessageId:$delivered"
            if (accept) outbox.value = outbox.value.map { it.copy(state = if (delivered) DeliveryState.Sent else DeliveryState.Refused) }
            return accept
        }
    }
}
