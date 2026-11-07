package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.ProtocolCompatibility
import persona.core.contracts.SessionsResponse
import kotlinx.serialization.json.*
import java.util.UUID

/** Optional real-hub JVM probe; environment values target only the coordinator's disposable loopback hub. */
class ProtocolCompatibilityLiveTest {
    @Test fun readsBaselineSessionsAndReplaysAMessageWithTheNewAndroidTransport() = runBlocking {
        val url = System.getenv("PERSONA_COMPAT_HUB")
        val token = System.getenv("PERSONA_COMPAT_TOKEN")
        assumeTrue("A disposable hub and credential were not supplied", !url.isNullOrBlank() && !token.isNullOrBlank())
        val base = url!!.toHttpUrl()
        assertTrue(base.host in setOf("127.0.0.1", "localhost", "::1"))
        HubHttpClient(null, null, "compatibility-probe").use { hub ->
            fun request(path: String) = Request.Builder().url(base.hubUrl(path)).header("Authorization", "Bearer $token")
            val list = hub.client.newCall(request("/api/v1/sessions").build()).awaitResponse().use {
                assertEquals(200, it.code)
                JsonCodec.decodeFromString<SessionsResponse>(it.body!!.string())
            }
            assertTrue("The isolated runtime must expose a baseline session fixture", list.sessions.isNotEmpty())
            val machine = System.getenv("PERSONA_COMPAT_MACHINE")
            val session = System.getenv("PERSONA_COMPAT_SESSION")
            if (!machine.isNullOrBlank() && !session.isNullOrBlank()) {
                val path = "/api/v1/sessions/$machine/$session/messages"
                val id = UUID.randomUUID().toString()
                val json = buildJsonObject {
                    put("text", "<persona-context kind=\"context\" title=\"Compatibility probe\" url=\"\">Synthetic baseline context</persona-context>\nCompatibility probe")
                    put("client_msg_id", id)
                }.toString()
                fun message() = request(path).apply {
                    ProtocolCompatibility.operationHeaders(id).forEach { (name, value) -> header(name, value) }
                }.post(compatibleRequestBody(path, json.toRequestBody("application/json".toMediaType()))!!).build()
                val first = hub.client.newCall(message()).awaitResponse().use { assertEquals(200, it.code); it.body!!.string() }
                val repeat = hub.client.newCall(message()).awaitResponse().use { assertEquals(200, it.code); it.body!!.string() }
                assertEquals(JsonCodec.parseToJsonElement(first), JsonCodec.parseToJsonElement(repeat))
                hub.client.newCall(request("/api/v1/ops/$id").build()).awaitResponse().use {
                    assertEquals(200, it.code)
                    val receipt = JsonCodec.parseToJsonElement(it.body!!.string()).jsonObject
                    assertEquals("done", receipt["state"]!!.jsonPrimitive.content)
                }
            }
            println("Compatibility JVM probe PASS: sessions=${list.sessions.size}; message replay=${!machine.isNullOrBlank() && !session.isNullOrBlank()}")
        }
    }
}
