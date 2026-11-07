package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.ConnectorReplyOutcome

/** A plugin connector's sign-in, thread pages and replies against a fake hub (synthetic data only). */
class HttpConnectorThreadTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpConnectorsManagement) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpConnectorsManagement({ path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }))
        }
    }

    @Test
    fun loginStepSendsStepAndValuesAndReadsThePrompt() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"step":"scan","kind":"qr","qr":{"size":2,"rows":["10","01"]},"interval":2,"hint":"scan it"}"""))
        val prompt = api.connectorLogin("example", "start", mapOf("server" to "https://chat.example.com"))
        val req = server.takeRequest()
        assertEquals("/api/v1/connectors/example/login", req.path)
        assertEquals("POST", req.method)
        assertEquals("""{"step":"start","values":{"server":"https://chat.example.com"}}""", req.body.readUtf8())
        assertEquals("scan", prompt.step)
        assertTrue(prompt.polled)
        assertEquals(listOf("10", "01"), prompt.qr?.rows)
    }

    @Test
    fun loginDoneAndLogout() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"done":true,"account":"wxid_example_a"}"""))
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        val done = api.connectorLogin("example", "", emptyMap())
        assertEquals("""{"step":"","values":{}}""", server.takeRequest().body.readUtf8())
        assertTrue(done.done)
        assertEquals("wxid_example_a", done.account)
        api.connectorLogout("example")
        assertEquals("/api/v1/connectors/example/logout", server.takeRequest().path)
    }

    @Test
    fun threadPageQueryAndParsing() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{
            "chat":{"id":"g1","name":"Example group","kind":"group","members":12,"reply":"chat.reply","extra":1},
            "messages":[
              {"id":"m1","at":"2026-11-05T08:00:00Z","sender":{"id":"wxid_example_a","name":"A"},"text":"hello",
               "media":[{"kind":"file","name":"plan.pdf","size":2048}]},
              {"id":"m2","at":"2026-11-05T08:01:00Z","sender":{"name":"Me"},"mine":true,"text":"ok",
               "reply_to":{"id":"m1","from":"A","text":"hello"},"edited":true}
            ],
            "before":"c2"}"""))
        val page = api.connectorThread("example", "room a/1", before = "c 1", limit = 20)
        assertEquals("/api/v1/connectors/example/thread?thread=room%20a%2F1&before=c%201&limit=20", server.takeRequest().path)
        assertEquals(12, page.chat.members)
        assertEquals("chat.reply", page.chat.reply)
        assertTrue(page.chat.isChat)
        assertEquals("c2", page.before)
        assertEquals("plan.pdf", page.messages[0].media.single().name)
        assertEquals(2048L, page.messages[0].media.single().size)
        assertEquals("m1", page.messages[1].replyTo?.id)
        assertTrue(page.messages[1].mine && page.messages[1].edited)
    }

    @Test
    fun firstPageHasNoBefore() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"chat":{"kind":"pr"},"messages":[]}"""))
        val page = api.connectorThread("example", "t1")
        assertEquals("/api/v1/connectors/example/thread?thread=t1&limit=30", server.takeRequest().path)
        assertFalse(page.chat.isChat)
        assertNull(page.before)
    }

    @Test
    fun replySentWithQuoteAndNote() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"ok":true,"result":{"message":"queued"}}"""))
        val outcome = api.connectorThreadReply("example", "g1", "chat.reply", "thanks", replyTo = "m1")
        val req = server.takeRequest()
        assertEquals("/api/v1/connectors/example/thread/reply", req.path)
        assertEquals("""{"thread":"g1","action":"chat.reply","text":"thanks","reply_to":"m1"}""", req.body.readUtf8())
        assertEquals(ConnectorReplyOutcome.Sent("queued"), outcome)
    }

    @Test
    fun replyDeclinedByTheGate() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(403).setBody("""{"error":"not approved","code":"declined"}"""))
        val outcome = api.connectorThreadReply("example", "g1", "chat.reply", "thanks", replyTo = null)
        assertFalse(server.takeRequest().body.readUtf8().contains("reply_to"))
        assertTrue(outcome is ConnectorReplyOutcome.Declined)
        assertEquals("not approved", (outcome as ConnectorReplyOutcome.Declined).reason)
    }

    @Test
    fun replyFailedWhenTheConnectorFails() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(502).setBody("""{"error":"connector unavailable"}"""))
        val outcome = api.connectorThreadReply("example", "g1", "chat.reply", "thanks", replyTo = null)
        assertTrue(outcome is ConnectorReplyOutcome.Failed)
        assertEquals("connector unavailable", (outcome as ConnectorReplyOutcome.Failed).reason)
    }

    @Test
    fun declinedIsNotAnExpiredPairing() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(403).setBody("""{"error":"not approved","code":"declined"}"""))
        server.enqueue(MockResponse().setResponseCode(403).setBody("""{"error":"forbidden"}"""))
        val declined = runCatching { api.connectorThread("example", "g1") }.exceptionOrNull() as HubFailure
        assertEquals(HubError.Server, declined.kind)
        val expired = runCatching { api.connectorThread("example", "g1") }.exceptionOrNull() as HubFailure
        assertEquals(HubError.PairingExpired, expired.kind)
    }
}
