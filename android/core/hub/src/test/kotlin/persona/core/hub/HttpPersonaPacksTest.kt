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
        assertEquals(emptyList<Any>(), (packs.list()).zhAny())
        assertEquals("png", (packs.file("doudou", "dot/idle.png").decodeToString()).zhAny())
        assertEquals(listOf("/api/v1/personas", "/api/v1/personas/doudou/dot/idle.png"), (asked).zhAny())
    }

    @Test fun looksAndTheChoiceGoToTheHub() = runBlocking {
        val put = mutableListOf<Pair<String, String>>()
        val listing = """{"looks":[{"id":"persona","source":"builtin","name":"团子","app_icon":"persona"},
            {"id":"aurora","source":"extension","name":"极光","app_icon":"persona","extension_name":"Example Looks","rev":"0123456789abcdef",
             "manifest":{"format":1,"id":"aurora","name":"极光","names":{"en":"Aurora"},"avatar":{"sizes":{"128":"a.png"},"svg":"a.svg","dark":{"sizes":{"128":"d.png"}}},
             "dot":{"states":{"idle":{"file":"i.png"}}},"source":{"kind":"extension","extension":"example-looks","extension_name":"Example Looks"}}}],
            "current":{"id":null,"effective":"persona"}}"""
        val packs = HttpPersonaPacks({ path, body -> put += path to body; ok(path, """{"id":"aurora","effective":"aurora"}""") }) { path -> ok(path, listing) }
        val looks = packs.looks()
        assertEquals(null, looks.current.id)
        val aurora = looks.looks.mapNotNull { it.pack }.single()
        assertEquals("aurora", aurora.id)
        assertEquals(true, aurora.usable)
        assertEquals("Example Looks", aurora.manifest.source?.extensionName)
        assertEquals("aurora", packs.choose("aurora", ifUnset = true).id)
        assertEquals(listOf("/api/v1/looks/current" to """{"id":"aurora","if_unset":true}"""), put)
    }

    @Test fun aHubWithoutLooksIsUnsupported() {
        val packs = HttpPersonaPacks { throw HubFailure(HubError.Server, "HTTP 404", status = 404) }
        assertThrows(PersonaPacksUnsupported::class.java) { runBlocking { packs.looks() } }
        assertThrows(PersonaPacksUnsupported::class.java) { runBlocking { packs.choose("aurora") } }
    }
}
