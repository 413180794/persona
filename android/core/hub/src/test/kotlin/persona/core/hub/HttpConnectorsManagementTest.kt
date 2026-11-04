package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.AddMailAccountRequest
import persona.core.contracts.api.GoogleFinish

class HttpConnectorsManagementTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpConnectorsManagement) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(
                server,
                HttpConnectorsManagement({ path, method, body ->
                    http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
                }),
            )
        }
    }

    @Test
    fun listConnectors() = withHub { server, api ->
        server.enqueue(
            MockResponse().setBody(
                """{"connectors":[
                {"id":"mail","name":"邮箱","status":"ok"},
                {"id":"lark","name":"飞书","status":"ok"},
                {"id":"github","name":"GitHub","status":"ok","account":"octocat"}
            ]}""",
            ),
        )
        val list = api.connectors(all = true)
        assertEquals("/api/v1/connectors?all=1", server.takeRequest().path)
        assertEquals(3, list.size)
        assertEquals("GitHub", list[2].name)
        assertEquals("octocat", list[2].account)
    }

    @Test
    fun setConnectorEnabled() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"id":"github","name":"GitHub","status":"ok","enabled":true}"""))
        val result = api.setConnectorEnabled("github", true)
        val req = server.takeRequest()
        assertEquals("/api/v1/connectors/github/settings", req.path)
        assertEquals("PUT", req.method)
        assertTrue(req.body.readUtf8().contains("\"enabled\":true"))
        assertEquals(true, result.enabled)
    }

    @Test
    fun mailAccountsAndTest() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"accounts":[{"id":"a1","email":"user@example.com","provider":"imap","status":"ok"}]}"""))
        val accounts = api.mailAccounts()
        assertEquals("/api/v1/mail/accounts", server.takeRequest().path)
        assertEquals(1, accounts.size)
        assertEquals("user@example.com", accounts[0].email)

        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        val testResult = api.testMailAccount("a1")
        assertEquals("/api/v1/mail/accounts/a1/test", server.takeRequest().path)
        assertTrue(testResult.ok)
    }

    @Test
    fun addMailAccountAndGoogleSignIn() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"account":{"id":"a2","email":"new@example.com","provider":"feishu","status":"ok"}}"""))
        val added = api.addMailAccount(AddMailAccountRequest(email = "new@example.com", provider = "feishu"))
        assertEquals("/api/v1/mail/accounts", server.takeRequest().path)
        assertEquals("a2", added.id)

        server.enqueue(MockResponse().setBody("""{"auth_url":"https://accounts.google.com/o/oauth2/auth","callback_scheme":"com.example.app","state":"st123"}"""))
        val start = api.googleSignInStart("hint@example.com")
        val startReq = server.takeRequest()
        assertEquals("/api/v1/mail/oauth/google/start", startReq.path)
        assertTrue(startReq.body.readUtf8().contains("hint@example.com"))
        assertEquals("st123", start.state)

        server.enqueue(MockResponse().setBody("""{"account":{"id":"a3","email":"gmail@example.com","provider":"gmail","auth":"oauth2","status":"ok"}}"""))
        val finished = api.googleSignInFinish(GoogleFinish("st123", "code123"))
        assertEquals("/api/v1/mail/oauth/google/finish", server.takeRequest().path)
        assertEquals("gmail@example.com", finished.email)
    }

    @Test
    fun larkStatusAndAccounts() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"configured":true,"connected":true,"name":"张三","last_event_at":1700000000}"""))
        val status = api.larkStatus()
        assertEquals("GET", server.takeRequest().method)
        assertTrue(status.connected)
        assertEquals("张三", status.name)

        server.enqueue(MockResponse().setBody("""{"accounts":[{"id":"u1","title":"张三","tenant":"Corp"}],"default":"u1"}"""))
        val accs = api.larkAccounts()
        assertEquals("/api/v1/lark/accounts", server.takeRequest().path)
        assertNotNull(accs)
        assertEquals(1, accs?.accounts?.size)
        assertEquals("Corp", accs?.accounts?.first()?.tenant)
    }
}
