package persona.core.hub

import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import org.junit.Assert.*
import org.junit.Test

class ProtocolCompatibilityMockServerTest {
    @Test fun discoveryFromEitherGenerationCannotStealAQueuedApiResponseOrItsRecordedRequest() {
        ProtocolCompatibilityMockServer().use { server ->
            val http = OkHttpClient()
            for (agent in listOf("jarvis-ports", "persona-ports")) {
                server.enqueue(MockResponse().setBody("API fixture"))
                http.newCall(Request.Builder().url(server.url("/")).header("User-Agent", agent).build()).execute().use {
                    assertEquals(404, it.code)
                }
                http.newCall(Request.Builder().url(server.url("/api/v1/sessions")).build()).execute().use {
                    assertEquals(200, it.code)
                    assertEquals("API fixture", it.body!!.string())
                }
                assertEquals("/api/v1/sessions", server.takeRequest().path)
            }
            assertEquals(2, server.requestCount)
        }
    }

    @Test fun uncertainCloseIsVerifiedWithTheQueuedSessionListEvenWhenDiscoveryInterleaves() = kotlinx.coroutines.runBlocking {
        for (agent in listOf("jarvis-ports", "persona-ports")) {
            ProtocolCompatibilityMockServer().use { server ->
                val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
                val actions = HttpChatActions { path, method, body ->
                    http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
                }
                val session = persona.core.contracts.Session("machine", "session/with#separator")
                fun discover() {
                    http.newCall(Request.Builder().url(server.url("/")).header("User-Agent", agent).build()).execute().use {
                        assertEquals(404, it.code)
                    }
                }
                server.enqueue(MockResponse().setResponseCode(502).setBody("""{"delivered":"unknown"}"""))
                discover()
                assertEquals(persona.core.contracts.api.ChatActionResult.Unknown, actions.execute(session, persona.core.contracts.api.ChatAction.Close))
                assertEquals("POST", server.takeRequest().method)
                server.enqueue(MockResponse().setBody("""{"sessions":[]}"""))
                discover()
                assertEquals(persona.core.contracts.api.ChatActionResult.Done, actions.execute(session, persona.core.contracts.api.ChatAction.Close))
                val verification = server.takeRequest()
                assertEquals("GET", verification.method)
                assertEquals("/api/v1/sessions", verification.path)
                assertEquals(2, server.requestCount)
            }
        }
    }

    @Test fun timedRequestReadsKeepTheirOriginalNonBlockingAndEmptyQueueSemantics() {
        ProtocolCompatibilityMockServer().use { server ->
            val http = OkHttpClient()
            server.enqueue(MockResponse().setBody("{}"))
            http.newCall(Request.Builder().url(server.url("/api/v1/sessions")).build()).execute().close()
            assertEquals("/api/v1/sessions", server.takeRequest(0, java.util.concurrent.TimeUnit.MILLISECONDS)?.path)
            assertNull(server.takeRequest(20, java.util.concurrent.TimeUnit.MILLISECONDS))
        }
    }

    @Test fun ordinaryRootRequestsAndApiRequestsWithProbeUserAgentsAreNotFiltered() {
        ProtocolCompatibilityMockServer().use { server ->
            val http = OkHttpClient()
            for ((path, agent) in listOf("/" to "ordinary-client", "/api/v1/sessions" to "persona-ports", "/?query=1" to "jarvis-ports")) {
                server.enqueue(MockResponse().setResponseCode(409).setBody("Expected refusal"))
                http.newCall(Request.Builder().url(server.url(path)).header("User-Agent", agent).build()).execute().use {
                    assertEquals(409, it.code)
                    assertEquals("Expected refusal", it.body!!.string())
                }
                assertEquals(path, server.takeRequest().path)
            }
            assertEquals(3, server.requestCount)
        }
    }
}
