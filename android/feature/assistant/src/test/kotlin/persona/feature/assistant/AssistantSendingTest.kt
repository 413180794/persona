package persona.feature.assistant

import persona.core.contracts.HubScope
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import persona.core.data.*
import persona.core.designsystem.PendingStatus
import persona.feature.chat.ChatDraftEditor
import persona.feature.chat.ChatComposerState
import persona.feature.chat.ComposerAttachment
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.TextFieldValue
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class AssistantSendingTest {
    @Test fun homeDraftDrainsOnExitRestoresAndKeepsPairingsSeparate() = runTest {
        class Drafts(generation: String) : DraftRepository {
            var saved = DraftSnapshot(HubScope("test", generation), DraftKey.Assistant())
            var fail = false
            override fun observe(key: DraftKey) = MutableStateFlow(saved)
            override suspend fun read(key: DraftKey) = saved
            override suspend fun save(key: DraftKey, text: String, expectedVersion: Long): DraftSnapshot {
                check(!fail && saved.version == expectedVersion)
                return saved.copy(text = text, version = expectedVersion + 1).also { saved = it }
            }
        }
        val firstPairing = Drafts("first")
        val editor = ChatDraftEditor(firstPairing, backgroundScope, DraftKey.Assistant())
        runCurrent()
        requireNotNull(editor.composer.value).editMessage("未发送的输入")
        editor.close()
        runCurrent()
        val restored = ChatDraftEditor(firstPairing, backgroundScope, DraftKey.Assistant())
        val other = ChatDraftEditor(Drafts("second"), backgroundScope, DraftKey.Assistant())
        runCurrent()
        assertEquals("未发送的输入", restored.composer.value?.value?.text)
        assertEquals("", other.composer.value?.value?.text)
        firstPairing.fail = true
        requireNotNull(restored.composer.value).editMessage("继续编辑")
        restored.changed(requireNotNull(restored.composer.value).draft)
        runCurrent()
        assertEquals("继续编辑", restored.composer.value?.value?.text)
        assertNotNull(restored.error.value)
        firstPairing.fail = false
        requireNotNull(restored.composer.value).editMessage("保存成功")
        restored.close()
        other.close()
        runCurrent()
        assertNull(restored.error.value)
        assertEquals("保存成功", firstPairing.saved.text)
    }

    @Test fun firstMessagesRestoreOfflineAndShareOneMainTargetWithExternalEntries() = runTest {
        val store = MemoryOutbox()
        val transport = Transport()
        val target = SessionKey("machine", "main")
        var creates = 0
        val resolver = object : MainConversationResolver {
            override suspend fun findMainConversation(): SessionKey? = null
            override suspend fun createMainConversation(): SessionKey { creates++; return target }
        }
        val scope = HubScope("test", "generation")
        val first = OutboxMessageSender(scope, store, transport, backgroundScope)
        first.connectMainConversation(resolver)
        val composer = ChatComposerState().apply {
            value = TextFieldValue("请看 这份文件", TextRange(3))
            insert("/workspace/report.txt", atSelection = true)
            attachments = listOf(ComposerAttachment("photo", "photo.jpg", "", true, "/workspace/photo.jpg"))
        }
        assertEquals("请看 /workspace/report.txt 这份文件", composer.value.text)
        val draftStore = object : DraftRepository {
            var saved = DraftSnapshot(scope, DraftKey.Assistant())
            override fun observe(key: DraftKey) = MutableStateFlow(saved)
            override suspend fun read(key: DraftKey) = saved
            override suspend fun save(key: DraftKey, text: String, expectedVersion: Long) =
                saved.copy(text = text, version = expectedVersion + 1).also { saved = it }
        }
        val editor = ChatDraftEditor(draftStore, backgroundScope, DraftKey.Assistant())
        runCurrent()
        requireNotNull(editor.composer.value).editMessage(composer.message())
        editor.close()
        runCurrent()
        val restoredEditor = ChatDraftEditor(draftStore, backgroundScope, DraftKey.Assistant())
        runCurrent()
        val restoredComposer = requireNotNull(restoredEditor.composer.value)
        assertEquals(composer.message(), restoredComposer.message())
        assertEquals("/workspace/photo.jpg", restoredComposer.attachments.single().path)
        sendAssistantMessage(first, first, null, restoredComposer.message(), "first")
        restoredEditor.close()
        first.sendToMain("分享的内容", "share")
        sendAssistantMessage(first, first, null, "交给 Persona 的内容", "handoff")
        runCurrent()
        assertEquals(listOf(composer.message(), "分享的内容", "交给 Persona 的内容"), first.outbox.value.map { it.text })
        assertTrue(first.outbox.value.all { it.session == PendingMainConversation && it.mainTarget })
        assertTrue(first.outbox.value.all { it.state.pendingStatus() == PendingStatus.Waiting })
        assertEquals(0, creates)
        assertTrue(transport.sent.isEmpty())
        first.close()

        val restored = OutboxMessageSender(scope, store, transport, backgroundScope)
        restored.connectMainConversation(resolver)
        runCurrent()
        assertEquals(3, restored.outbox.value.size)
        transport.readyToSend.value = true
        runCurrent()
        assertEquals(1, creates)
        assertEquals(listOf("first", "share", "handoff"), transport.sent.map { it.clientMessageId })
        assertTrue(transport.sent.all { it.session == target })
        assertTrue(restored.outbox.value.all { it.session == target })
        restored.close()
    }

    @Test fun enqueueFailureRetainsInputAndRetryUsesSameIdWithoutDuplicatingOrRedirectingBranches() = runTest {
        val store = MemoryOutbox().apply { fail = true }
        val transport = Transport()
        val sender = OutboxMessageSender(HubScope("test", "generation"), store, transport, backgroundScope)
        var editor = "保留这句"
        try {
            sendAssistantMessage(sender, sender, null, editor, "retry")
            editor = ""
            fail("The failed commit must propagate")
        } catch (_: java.io.IOException) { }
        assertEquals("保留这句", editor)
        assertTrue(store.entries.value.isEmpty())
        store.fail = false
        repeat(2) { sendAssistantMessage(sender, sender, null, editor, "retry") }
        assertEquals(1, store.entries.value.size)
        assertEquals("retry", store.entries.value.single().clientMessageId)
        val branch = SessionKey("machine", "branch")
        sendAssistantMessage(sender, sender, branch, "继续旁支", "branch-message")
        assertEquals(branch, store.entries.value.last().session)
        assertFalse(store.entries.value.last().mainTarget)
        sender.close()
    }

    private class Transport : MessageTransport {
        override val readyToSend = MutableStateFlow(false)
        override val supportsClientOps = true
        val sent = mutableListOf<MessageRequest>()
        override suspend fun send(request: MessageRequest): MessageTransportResult {
            sent += request
            return MessageTransportResult.Accepted
        }
    }

    private class MemoryOutbox : OutboxStore {
        override val entries = MutableStateFlow<List<OutboxEntry>>(emptyList())
        var fail = false
        private var main: SessionKey? = null
        private var claimed = false
        override suspend fun enqueue(entry: OutboxEntry) {
            if (fail) throw java.io.IOException("Storage unavailable")
            if (entries.value.none { it.clientMessageId == entry.clientMessageId }) entries.value += entry
        }
        override suspend fun mainConversation() = main
        override suspend fun claimMainCreation(): Boolean = (!claimed).also { claimed = true }
        override suspend fun bindMainConversation(session: SessionKey) {
            main = session
            entries.value = entries.value.map { if (it.mainTarget) it.copy(session = session) else it }
        }
        override suspend fun transition(id: String, from: Set<OutboxState>, to: OutboxState, error: String?): Boolean {
            val entry = entries.value.firstOrNull { it.clientMessageId == id && it.state in from } ?: return false
            entries.value = entries.value.map { if (it == entry) it.copy(state = to, error = error) else it }
            return true
        }
        override suspend fun recoverInterrupted() = Unit
    }
}
