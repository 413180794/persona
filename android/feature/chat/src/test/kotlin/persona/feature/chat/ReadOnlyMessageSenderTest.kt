package persona.feature.chat

import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

class ReadOnlyMessageSenderTest {
    @Test fun historicalReaderCannotSendRetryOrChangeDelivery() = runTest {
        var writes = 0
        val source = object : MessageSender {
            override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) { writes++ }
            override suspend fun retry(clientMessageId: String): Boolean { writes++; return true }
            override suspend fun cancel(clientMessageId: String): Boolean { writes++; return true }
            override suspend fun confirm(clientMessageId: String, delivered: Boolean): Boolean { writes++; return true }
        }
        val reader = ReadOnlyMessageSender(source, "历史会话只读，请先继续这个会话")
        try {
            reader.send(SessionKey("machine", "history"), "消息", "operation")
            fail("Historical messages must be blocked")
        } catch (_: IllegalStateException) { }
        assertFalse(reader.retry("operation"))
        assertFalse(reader.cancel("operation"))
        assertFalse(reader.confirm("operation", true))
        assertEquals(0, writes)
    }
}
