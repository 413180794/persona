package persona.core.hub

import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.long
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.JsonCodec

class HttpTranslationTransportTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpTranslationTransport) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpTranslationTransport { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            })
        }
    }

    @Test fun textsInOrderAndBlankWhereTheModelGaveNothing() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"texts":["你好","",null]}"""))
        assertEquals(listOf("你好", "", ""), (api.translate(listOf("Hello", "?", "x"), "zh-Hans")).zhAny())
        val req = server.takeRequest()
        assertEquals("POST", (req.method).zhAny())
        assertEquals("/api/v1/translate", (req.path).zhAny())
        val body = JsonCodec.parseToJsonElement(req.body.readUtf8()).jsonObject
        assertEquals(listOf("Hello", "?", "x"), (body.getValue("texts").jsonArray.map { it.jsonPrimitive.content }).zhAny())
        assertEquals("zh-Hans", (body.getValue("target").jsonPrimitive.content).zhAny())
    }

    @Test fun aMailByReference() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"subject":"季度回顾","text":"正文","clipped":true}"""))
        val t = api.translateMail("acc1", 504, "INBOX", "zh-Hans")
        assertEquals("季度回顾", (t.subject).zhAny())
        assertTrue(t.clipped)
        val body = JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals("acc1", (body.getValue("account_id").jsonPrimitive.content).zhAny())
        assertEquals(504L, body.getValue("uid").jsonPrimitive.long)
        assertEquals("INBOX", (body.getValue("mailbox").jsonPrimitive.content).zhAny())
    }

    @Test fun failuresAndUnreadableAnswers() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(503).setBody("""{"error":"no model"}"""))
        try { api.translate(listOf("Hi"), "zh-Hans"); fail() } catch (e: HubFailure) { assertEquals(503, e.status) }
        server.enqueue(MockResponse().setBody("not json"))
        try { api.translateMail("a", 1, "INBOX", "en"); fail() } catch (e: HubFailure) { assertEquals(HubError.Protocol, e.kind) }
    }
}
