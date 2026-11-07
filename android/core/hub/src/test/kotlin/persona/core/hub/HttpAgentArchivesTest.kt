package persona.core.hub

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.SessionKey

class HttpAgentArchivesTest {
    @Test fun listsBothAgentsAndWritesEncodedAuthenticatedPaths() = runBlocking {
        ProtocolCompatibilityMockServer().use { server ->
            val api = HttpAgentArchives(server.url("/hub/").toString(), "secret", OkHttpClient(), { true })
            server.enqueue(MockResponse().setBody("""{"sessions":[
                {"machine_id":"desktop","session_id":"codex-thread","agent":"codex"},
                {"machine_id":"desktop","session_id":"claude-thread","agent":"claude"}
            ]}"""))
            assertEquals(listOf("codex", "claude"), api.list().map { it.agent })
            assertEquals("/hub/api/v1/sessions/archived", server.takeRequest().path)
            for (action in listOf("archive", "unarchive")) {
                server.enqueue(MockResponse().setBody("""{"ok":true}"""))
                val key = SessionKey("machine/id", "thread#id")
                if (action == "archive") api.archive(key) else api.restore(key)
                val request = server.takeRequest()
                assertEquals("POST", request.method)
                assertEquals("/hub/api/v1/sessions/machine%2Fid/thread%23id/$action", request.path)
                assertEquals("Bearer secret", request.getHeader("Authorization"))
                assertEquals("{}", request.body.readUtf8())
            }
        }
    }

    @Test fun failuresAndMissingAcknowledgementRemainFailures() = runBlocking {
        ProtocolCompatibilityMockServer().use { server ->
            val api = HttpAgentArchives(server.url("/").toString(), "secret", OkHttpClient(), { true })
            for (status in listOf(403, 409, 503)) {
                server.enqueue(MockResponse().setResponseCode(status).setBody("""{"error":"refused"}"""))
                val error = runCatching { api.archive(SessionKey("m", "s")) }.exceptionOrNull()
                assertEquals(status, (error as? HubFailure)?.status)
                server.takeRequest()
            }
            server.enqueue(MockResponse().setBody("""{"ok":false}"""))
            assertTrue(runCatching { api.restore(SessionKey("m", "s")) }.isFailure)
            server.takeRequest()
            server.enqueue(MockResponse().setResponseCode(503))
            assertTrue(runCatching { api.list() }.exceptionOrNull() is HubFailure)
            server.takeRequest()
            assertEquals(5, server.requestCount)
        }
    }

    @Test fun unsupportedAndReplacedPairingsNeverSendRequests() = runBlocking {
        ProtocolCompatibilityMockServer().use { server ->
            var supported = false
            var current = true
            val api = HttpAgentArchives(server.url("/").toString(), "secret", OkHttpClient(), { supported }, { current })
            assertFalse(api.supported)
            assertTrue(runCatching { api.list() }.exceptionOrNull() is IllegalStateException)
            supported = true
            current = false
            assertTrue(api.supported)
            assertTrue(runCatching { api.archive(SessionKey("m", "s")) }.exceptionOrNull() is CancellationException)
            assertEquals(0, server.requestCount)
        }
    }
}
