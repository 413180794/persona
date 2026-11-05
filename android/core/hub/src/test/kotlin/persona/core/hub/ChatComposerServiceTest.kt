package persona.core.hub

import persona.core.contracts.HubScope
import persona.core.contracts.JsonCodec
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
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
                assertEquals("abc", (buffer.readUtf8()).zhAny())
                assertTrue(path.contains("name="))
                200 to """{"path":"/tmp/test.txt"}"""
            } else {
                val fields = JsonCodec.parseToJsonElement(buffer.readUtf8()).jsonObject
                assertEquals(headers["X-Jarvis-Op-Id"], fields["client_msg_id"]!!.jsonPrimitive.content)
                assertEquals("消息", (fields["text"]!!.jsonPrimitive.content).zhAny())
                attempts++
                // The retry goes under a new operation id: the hub would answer "operation" from its first try.
                assertEquals(attempts == 1, headers["X-Jarvis-Op-Id"] == "operation")
                if (attempts == 1) {
                    assertNull(fields["clear_draft"])
                    409 to """{"error":"电脑上有草稿","code":"input_draft","delivered":"none","draft":"电脑草稿","clearable":"电脑草稿"}"""
                } else {
                    assertEquals("电脑草稿", (fields["clear_draft"]!!.jsonPrimitive.content).zhAny())
                    200 to "{}"
                }
            }
            Response.Builder().request(Request.Builder().url("https://example.com/").build())
                .protocol(Protocol.HTTP_1_1).code(code).message("test").body(response.toResponseBody()).build()
        })
        val file = File.createTempFile("composer-test-", ".txt")
        try {
            file.writeText("abc")
            assertEquals("/tmp/test.txt", (service.upload(session, "test.txt", file)).zhAny())
        } finally { file.delete() }
        service.send(MessageRequest(scope, session, "消息", "operation"))
        assertEquals("电脑草稿", (service.deliveries.value["operation"]!!.refusal!!.draft).zhAny())
        assertEquals(DeliveryState.Sent, service.retryRefused("operation", true).state)
        try {
            service.retryRefused("operation", true)
            fail("A delivered message must not be retried")
        } catch (_: ComposerOperationException) { }
        assertEquals(2, attempts)
    }

    @Test fun clearAndSendGoesUnderANewOperationIdTheHubHasNotAnswered() = runBlocking {
        val scope = HubScope("test-hub", "generation")
        val session = SessionKey("machine", "session")
        val transport = object : MessageTransport {
            override val readyToSend = MutableStateFlow(true)
            override val supportsClientOps = true
            override suspend fun send(request: MessageRequest): MessageTransportResult = error("Unexpected delegate send")
        }
        // Like the hub's client ops: the first answer per operation id, op_id_reused when its parameters change.
        val receipts = mutableMapOf<String, Pair<String, Pair<Int, String>>>()
        val ops = mutableListOf<String>()
        val service = HttpChatComposerService(scope, transport, request = { _, body, headers ->
            val buffer = Buffer()
            body.writeTo(buffer)
            val raw = buffer.readUtf8()
            val op = headers.getValue("X-Jarvis-Op-Id")
            ops += op
            assertEquals(op, JsonCodec.parseToJsonElement(raw).jsonObject["client_msg_id"]!!.jsonPrimitive.content)
            val params = JsonCodec.parseToJsonElement(raw).jsonObject.filterKeys { it != "client_msg_id" }.toString()
            val (code, response) = receipts[op]?.let { (seen, answer) ->
                if (seen == params) answer else 409 to """{"code":"op_id_reused"}"""
            } ?: (if (receipts.isEmpty()) 409 to """{"error":"电脑上有草稿","code":"input_draft","delivered":"none","draft":"电脑草稿","clearable":"电脑草稿"}"""
                else 200 to "{}").also { receipts[op] = params to it }
            Response.Builder().request(Request.Builder().url("https://example.com/").build())
                .protocol(Protocol.HTTP_1_1).code(code).message("test").body(response.toResponseBody()).build()
        })
        service.send(MessageRequest(scope, session, "消息", "operation"))
        assertEquals(DeliveryState.Refused, service.deliveries.value["operation"]!!.state)
        assertEquals(DeliveryState.Sent, service.retryRefused("operation", true).state)
        assertEquals(2, ops.size)
        assertNotEquals(ops[0], ops[1])
        assertEquals(DeliveryState.Sent, service.deliveries.value["operation"]!!.state)
    }
}
