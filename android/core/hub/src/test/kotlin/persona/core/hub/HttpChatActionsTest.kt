package persona.core.hub

import persona.core.contracts.*
import persona.core.contracts.api.*
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpChatActionsTest {
    @Test fun rejectedMenuOperationRetainsStructuredFailure() = runBlocking {
        val failure = HubFailure(HubError.Server, "提示文字", status = 409, code = "stale_session", delivered = "none", retryable = false)
        val actions = HttpChatActions { _, _, _ -> throw failure }
        val result = actions.execute(Session("machine", "session"), ChatAction.Close) as ChatActionResult.Failed
        assertSame(failure, result.cause)
    }

    @Test fun menuRequestsPreserveMuteSettingsEncodePathsAndVerifyUncertainClose() = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            val actions = HttpChatActions { path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute().also {
                    if (!it.isSuccessful) throw it.use { response -> HubFailure.from(response) }
                }
            }
            val session = Session(machineId = "machine", sessionId = "session/with#separator", spaceName = "space")
            server.enqueue(MockResponse().setBody("{}"))
            assertEquals(ChatActionResult.Done, actions.execute(session, ChatAction.Rename("新标题")))
            val rename = server.takeRequest()
            assertEquals("/api/v1/sessions/machine/session%2Fwith%23separator/rename", rename.path)
            assertEquals("新标题", JsonCodec.parseToJsonElement(rename.body.readUtf8()).jsonObject["title"]!!.jsonPrimitive.content)
            server.enqueue(MockResponse().setBody("""{"spaces":["machine/space"],"sessions":["machine/other"],"allowed":[],"future":true}"""))
            server.enqueue(MockResponse().setBody("{}"))
            assertEquals(ChatActionResult.Done, actions.execute(session, ChatAction.Mute(false)))
            assertEquals("GET", server.takeRequest().method)
            val mute = server.takeRequest()
            assertEquals("PUT", mute.method)
            val payload = JsonCodec.parseToJsonElement(mute.body.readUtf8()).jsonObject
            assertEquals("machine/other", payload["sessions"]!!.jsonArray.single().jsonPrimitive.content)
            assertEquals("machine/space", payload["spaces"]!!.jsonArray.single().jsonPrimitive.content)
            assertEquals(session.key.path, payload["allowed"]!!.jsonArray.single().jsonPrimitive.content)
            assertEquals(JsonPrimitive(true), payload["future"])
            server.enqueue(MockResponse().setResponseCode(502).setBody("""{"delivered":"unknown"}"""))
            assertEquals(ChatActionResult.Unknown, actions.execute(session, ChatAction.Close))
            server.takeRequest()
            server.enqueue(MockResponse().setBody("""{"sessions":[]}"""))
            assertEquals(ChatActionResult.Done, actions.execute(session, ChatAction.Close))
            assertEquals("GET", server.takeRequest().method)
            assertEquals(5, server.requestCount)
        }
    }

    private fun actions(server: MockWebServer): HttpChatActions {
        val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
        return HttpChatActions { path, method, body ->
            http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
        }
    }

    @Test fun attachPostsToTheSessionAndVerifiesTheAttachedFlag() = runBlocking {
        MockWebServer().use { server ->
            val actions = actions(server)
            val session = Session(machineId = "m", sessionId = "opaque", sourceKind = "codex_app_server",
                capabilities = SessionCapabilities(read = true, send = true, attach = true))
            server.enqueue(MockResponse().setBody("{}"))
            assertEquals(ChatActionResult.Done, actions.execute(session, ChatAction.Attach))
            val request = server.takeRequest()
            assertEquals("POST", request.method)
            assertEquals("/api/v1/sessions/m/opaque/attach", request.path)
            // A lost answer is verified against the session list, not sent again.
            server.enqueue(MockResponse().setResponseCode(502).setBody("""{"delivered":"unknown"}"""))
            assertEquals(ChatActionResult.Unknown, actions.execute(session, ChatAction.Attach))
            server.takeRequest()
            server.enqueue(MockResponse().setBody("""{"sessions":[{"machine_id":"m","session_id":"opaque","attached":true}]}"""))
            assertEquals(ChatActionResult.Done, actions.verify(session, ChatAction.Attach))
            assertEquals("GET", server.takeRequest().method)
            assertEquals(3, server.requestCount)
        }
    }

    @Test fun queueEditsSendEveryIndexKeptAndReturnWhatWasQueued() = runBlocking {
        MockWebServer().use { server ->
            val actions = actions(server)
            val session = Session(machineId = "m", sessionId = "s/1")
            server.enqueue(MockResponse().setBody("""{"queue":["先跑测试","再提交",1]}"""))
            assertEquals(listOf("先跑测试", "再提交"), actions.editQueue(session, listOf(1)))
            val edit = server.takeRequest()
            assertEquals("POST", edit.method)
            assertEquals("/api/v1/sessions/m/s%2F1/queue", edit.path)
            assertEquals("""{"keep":[1]}""", edit.body.readUtf8())
            // Deleting the only queued message keeps nothing: the empty list goes out.
            server.enqueue(MockResponse().setBody("""{"queue":["只有一条"]}"""))
            actions.editQueue(session, emptyList())
            assertEquals("""{"keep":[]}""", server.takeRequest().body.readUtf8())
            server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"队列已经被取走","code":"queue_changed","delivered":"none"}"""))
            val failure = runCatching { actions.editQueue(session, emptyList()) }.exceptionOrNull() as HubFailure
            assertEquals("queue_changed", failure.code)
            assertEquals(3, server.requestCount)
        }
    }

    @Test fun infoReadsTheSessionRoute() = runBlocking {
        MockWebServer().use { server ->
            val actions = actions(server)
            server.enqueue(MockResponse().setBody("""{"session_id":"s","model":"claude-opus","context_tokens":48200,"turns":6}"""))
            val info = actions.info(Session(machineId = "m", sessionId = "s"))
            assertEquals("claude-opus", info.model)
            assertEquals(48200L, info.contextTokens)
            assertEquals(6, info.turns)
            val request = server.takeRequest()
            assertEquals("GET", request.method)
            assertEquals("/api/v1/sessions/m/s/info", request.path)
            server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"session not found"}"""))
            assertEquals(404, (runCatching { actions.info(Session(machineId = "m", sessionId = "gone")) }.exceptionOrNull() as HubFailure).status)
        }
    }
}
