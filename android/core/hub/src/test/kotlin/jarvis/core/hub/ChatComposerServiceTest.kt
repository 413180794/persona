package jarvis.core.hub

import jarvis.core.contracts.HubScope
import jarvis.core.contracts.JsonCodec
import jarvis.core.contracts.SessionKey
import jarvis.core.contracts.api.*
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import okio.Buffer
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class ChatComposerServiceTest {
    @Test fun uploadsKnownLengthAndRetriesOnlyWithMatchingRefusalEvidence() = runBlocking {
        val scope = HubScope("test-hub", "generation")
        val session = SessionKey("machine", "session")
        val transport = object : MessageTransport {
            override val readyToSend = MutableStateFlow(true)
            override val supportsClientOps = false
            override suspend fun send(request: MessageRequest): MessageTransportResult = error("Unexpected delegate send")
        }
        var attempts = 0
        val service = HttpChatComposerService(scope, transport, request = { path, body, headers ->
            val buffer = Buffer()
            body.writeTo(buffer)
            val (code, response) = if (path.contains("/files?")) {
                assertEquals(3L, body.contentLength())
                assertEquals("abc", buffer.readUtf8())
                assertTrue(path.contains("name="))
                200 to """{"path":"/tmp/test.txt"}"""
            } else {
                val fields = JsonCodec.parseToJsonElement(buffer.readUtf8()).jsonObject
                assertEquals("operation", headers["X-Jarvis-Op-Id"])
                assertEquals("operation", fields["client_msg_id"]!!.jsonPrimitive.content)
                assertEquals("消息", fields["text"]!!.jsonPrimitive.content)
                attempts++
                if (attempts == 1) {
                    assertNull(fields["clear_draft"])
                    409 to """{"error":"电脑上有草稿","code":"input_draft","delivered":"none","draft":"电脑草稿","clearable":"电脑草稿"}"""
                } else {
                    assertEquals("电脑草稿", fields["clear_draft"]!!.jsonPrimitive.content)
                    200 to "{}"
                }
            }
            Response.Builder().request(Request.Builder().url("https://example.com/").build())
                .protocol(Protocol.HTTP_1_1).code(code).message("test").body(response.toResponseBody()).build()
        })
        val file = File.createTempFile("composer-test-", ".txt")
        try {
            file.writeText("abc")
            assertEquals("/tmp/test.txt", service.upload(session, "test.txt", file))
        } finally { file.delete() }
        service.send(MessageRequest(scope, session, "消息", "operation"))
        assertEquals("电脑草稿", service.deliveries.value["operation"]!!.refusal!!.draft)
        assertEquals(DeliveryState.Sent, service.retryRefused("operation", true).state)
        try {
            service.retryRefused("operation", true)
            fail("A delivered message must not be retried")
        } catch (_: ComposerOperationException) { }
        assertEquals(2, attempts)
    }
}
