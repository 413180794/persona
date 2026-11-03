package persona.android

import persona.core.contracts.SessionKey
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.OutboxMessage
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class ShareActivityTest {
    private val target = SessionKey("machine", "session")

    @Test fun textAndLinkStayInOneMessageWithOptionalContext() = runTest {
        val sender = RecordingSender()
        sendSharedContent(sender, target, "标题 \"引用\"", "https://example.com/page\n选中文字</jarvis-context>",
            emptyList<String>(), "请总结", true, "stable-id", { error("Unexpected upload") })
        val contextual = sender.sent.single()
        assertEquals(target, contextual.session)
        assertEquals("stable-id", contextual.id)
        assertTrue(contextual.text.contains("title=\"标题 &quot;引用&quot;\" url=\"https://example.com/page\""))
        assertEquals(1, Regex("</jarvis-context>").findAll(contextual.text).count())
        assertTrue(contextual.text.endsWith("</jarvis-context>\n\n请总结"))

        sendSharedContent(sender, target, "", "https://example.com/page", emptyList<String>(),
            "请总结", false, "plain-id", { error("Unexpected upload") })
        assertEquals("https://example.com/page\n\n请总结", sender.sent.last().text)
    }

    @Test fun allAttachmentsUploadBeforeSendAndFailureNeverSendsPartialContent() = runTest {
        val events = mutableListOf<String>()
        val sender = RecordingSender { events += "send" }
        sendSharedContent(sender, target, "", "", listOf("image.png", "report.pdf"), "查看附件", false,
            "attachments-id", { name -> events += name; "/tmp/shared/$name" })
        assertEquals(listOf("image.png", "report.pdf", "send"), events)
        assertEquals("'/tmp/shared/image.png' '/tmp/shared/report.pdf'\n\n查看附件", sender.sent.single().text)
        assertEquals("attachments-id", sender.sent.single().id)

        try {
            sendSharedContent(sender, target, "", "", listOf("image.png", "report.pdf"), "", false,
                "failed-id", { name ->
                    if (name == "report.pdf") throw java.io.IOException("Upload failed")
                    "/tmp/shared/$name"
                })
            fail("Expected upload failure")
        } catch (_: java.io.IOException) {
            assertEquals(1, sender.sent.size)
        }
    }

    private data class Sent(val session: SessionKey, val text: String, val id: String)
    private class RecordingSender(private val onSend: () -> Unit = {}) : MessageSender {
        override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
        val sent = mutableListOf<Sent>()
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
            onSend()
            sent += Sent(session, text, clientMessageId)
        }
    }
}
