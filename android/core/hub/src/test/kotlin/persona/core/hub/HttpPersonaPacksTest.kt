package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import persona.core.contracts.api.PersonaPacksUnsupported

class HttpPersonaPacksTest {
    private fun ok(path: String, body: String) = Response.Builder().request(Request.Builder().url("https://hub.test$path").build())
        .protocol(Protocol.HTTP_1_1).code(200).message("OK").body(body.toResponseBody()).build()

    @Test fun anOlderHubWithoutTheRouteIsUnsupported() {
        // The connection throws for a non-2xx answer before the transport sees it.
        val packs = HttpPersonaPacks { throw HubFailure(HubError.Server, "HTTP 404", status = 404) }
        assertThrows(PersonaPacksUnsupported::class.java) { runBlocking { packs.list() } }
        val down = HttpPersonaPacks { throw HubFailure(HubError.Server, "HTTP 500", status = 500) }
        assertThrows(HubFailure::class.java) { runBlocking { down.list() } }
    }

    @Test fun filesAreFetchedByEncodedPath() = runBlocking {
        val asked = mutableListOf<String>()
        val packs = HttpPersonaPacks { path -> asked += path; ok(path, if (path.endsWith("personas")) """{"personas":[]}""" else "png") }
        assertEquals(emptyList<Any>(), packs.list())
        assertEquals("png", packs.file("doudou", "dot/idle.png").decodeToString())
        assertEquals(listOf("/api/v1/personas", "/api/v1/personas/doudou/dot/idle.png"), asked)
    }
}
