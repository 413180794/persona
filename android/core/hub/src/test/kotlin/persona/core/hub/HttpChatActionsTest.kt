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
}
