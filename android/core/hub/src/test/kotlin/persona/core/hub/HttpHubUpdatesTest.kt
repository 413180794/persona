package persona.core.hub

import persona.core.contracts.api.HubUpdatesUnsupported
import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import persona.core.hub.ProtocolCompatibilityMockServer as MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpHubUpdatesTest {
    // Like the pairing's connection: a status that is not 2xx is thrown as the hub's error before the client sees it.
    private fun withHub(block: suspend (MockWebServer, HttpHubUpdates) -> Unit): Unit = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpHubUpdates({ path, method, body ->
                val response = http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
                if (!response.isSuccessful) throw response.use { HubFailure.from(it) }
                response
            }))
        }
    }

    // What the hub's updateView sends (server/internal/hub/selfupdate.go), plus a key this app does not know.
    private val waiting = """{"version":"1.42.0","build":420,"channel":"release","can_update":true,
        "latest":{"version":"1.43.0","build":431,"released":"2026-11-04T08:00:00Z","notes":"更快的同步","newer":true,"bad":false,
          "held":"这一版的代码不含 hub 部署时的提交"},
        "state":"waiting_idle","busy":[{"machine_id":"m1","machine":"工作站","session_id":"s1","title":"重构登录"}],
        "auto":true,"auto_notice":false,"last_check":"2026-11-04T09:00:00Z","confirmed":true,"future":{"x":1}}"""

    @Test fun readsTheStatusAndToleratesWhatItDoesNotKnow() = withHub { server, api ->
        server.enqueue(MockResponse().setBody(waiting))
        val s = api.status()
        val request = server.takeRequest()
        assertEquals("GET", (request.method).zhAny())
        assertEquals("/api/v1/hub/update", (request.path).zhAny())
        assertEquals("1.42.0", (s.version).zhAny())
        assertEquals(420L, s.build)
        assertTrue(s.canUpdate && s.auto && !s.autoNotice)
        assertEquals("waiting_idle", (s.state).zhAny())
        assertTrue(s.working)
        assertEquals("1.43.0", (s.latest?.version).zhAny())
        assertEquals(431L, s.latest?.build)
        assertTrue(s.latest!!.newer)
        assertEquals("这一版的代码不含 hub 部署时的提交", (s.latest?.held).zhAny())
        assertEquals("工作站", (s.busy.single().machine).zhAny())
        assertEquals("s1", (s.busy.single().sessionId).zhAny())
        assertEquals("2026-11-04T09:00:00Z", (s.lastCheck).zhAny())
    }

    @Test fun aDockerHubSaysItsImageAndOldFieldsMayBeMissing() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"version":"1.40.0","channel":"docker","can_update":false,"why_not":"Docker 部署",
            "latest":{"version":"1.41.0","build":0,"newer":true,"bad":false},"state":"available","busy":null,
            "image":{"name":"ghcr.io/example/persona","tag":"1.40.0","pinned":true}}"""))
        val s = api.status()
        assertEquals("docker", (s.channel).zhAny())
        assertEquals("Docker 部署", (s.whyNot).zhAny())
        assertEquals(emptyList<Any>(), (s.busy).zhAny())
        assertTrue(s.confirmed) // absent: the hub has nothing to confirm
        assertEquals("ghcr.io/example/persona", (s.image?.name).zhAny())
        assertTrue(s.image!!.pinned)
    }

    @Test fun aHubFromBeforeUpdatesInTheAppIsUnsupported() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(404).setBody("404 page not found\n"))
        assertThrows(HubUpdatesUnsupported::class.java) { runBlocking { api.status() } }
    }

    @Test fun checksStartsAndChangesTheSettings() = withHub { server, api ->
        repeat(5) { server.enqueue(MockResponse().setBody(waiting)) }
        api.check()
        server.takeRequest().let { assertEquals("POST /api/v1/hub/update/check {}", ("${it.method} ${it.path} ${it.body.readUtf8()}").zhAny()) }
        api.start(now = false)
        server.takeRequest().let { assertEquals("POST /api/v1/hub/update {}", ("${it.method} ${it.path} ${it.body.readUtf8()}").zhAny()) }
        api.start(now = true)
        server.takeRequest().let { assertEquals("""POST /api/v1/hub/update {"now":true}""", ("${it.method} ${it.path} ${it.body.readUtf8()}").zhAny()) }
        api.settings(auto = false)
        server.takeRequest().let { assertEquals("""PUT /api/v1/hub/update/settings {"auto":false}""", ("${it.method} ${it.path} ${it.body.readUtf8()}").zhAny()) }
        api.settings(noticeSeen = true)
        server.takeRequest().let { assertEquals("""PUT /api/v1/hub/update/settings {"auto_notice_seen":true}""", ("${it.method} ${it.path} ${it.body.readUtf8()}").zhAny()) }
    }

    @Test fun aRefusalCarriesTheHubsWords() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"正在更新"}"""))
        val failure = assertThrows(HubFailure::class.java) { runBlocking { api.start(now = false) } }
        assertEquals(409, failure.status)
        assertEquals("正在更新", (failure.message).zhAny())
    }

    @Test fun readsTheHistory() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"history":[
            {"from_version":"1.41.0","to_version":"1.42.0","to_build":420,"started":"2026-11-03T10:00:00Z","ended":"2026-11-03T10:02:00Z","result":"updated","auto":true},
            {"from_version":"1.40.0","to_version":"1.41.0","to_build":410,"result":"rolled_back","reason":"新版本没起来","auto":false}]}"""))
        val history = api.history()
        assertEquals("/api/v1/hub/update/history", (server.takeRequest().path).zhAny())
        assertEquals(listOf("updated", "rolled_back"), (history.map { it.result }).zhAny())
        assertEquals(420L, history[0].toBuild)
        assertTrue(history[0].auto)
        assertEquals("新版本没起来", (history[1].reason).zhAny())
        server.enqueue(MockResponse().setBody("""{"history":null}"""))
        assertEquals(emptyList<Any>(), (api.history()).zhAny())
    }

    @Test fun aBodyItCannotReadIsTheProtocols() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("<html>"))
        val failure = assertThrows(HubFailure::class.java) { runBlocking { api.status() } }
        assertEquals(HubError.Protocol, failure.kind)
    }
}
