package persona.core.hub

import kotlinx.coroutines.runBlocking
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.serialization.json.*

class HttpExtensionModelTest {
    private fun withHub(legacy: Boolean = false, block: suspend (MockWebServer, HttpExtensionManagement) -> Unit) = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpExtensionManagement({ path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }, legacy = legacy))
        }
    }

    @Test fun contributionSwitchesAndSandboxReadBothRoutes() {
        for (legacy in listOf(false, true)) withHub(legacy) { server, api ->
            val root = if (legacy) "plugins" else "extensions"
            server.enqueue(MockResponse().setBody("""{"id":"example","kind":"ui","contributions":[
                {"point":"ui","id":"notes","title":"笔记","enabled":false,"can_disable":true},
                {"point":"connectors","id":"notes","enabled":true,"can_disable":false,"via":"connector"}],
                "sandbox":{"enabled":true,"default":true,"chosen":false,"active":false,"why":"当前机器无法隔离",
                    "egress":{"allowed":7,"denied":2,"last_denied":"example.com"}}}"""))
            val detail = api.setContribution("example", "ui", "notes", false)
            assertEquals(2, detail.contributions!!.size)
            assertFalse(detail.extension.sandbox!!.active)
            assertEquals(2L, detail.extension.sandbox!!.egress!!.denied)
            server.takeRequest().let {
                assertEquals("/api/v1/$root/example/-/contributions/ui/notes", it.path)
                assertEquals("PUT", it.method)
                assertEquals("""{"enabled":false}""", it.body.readUtf8())
            }
            server.enqueue(MockResponse().setBody("""{"id":"example"}"""))
            api.setContribution("example", "example-export.notes", "new", true)
            assertEquals("/api/v1/$root/example/-/contributions/example-export.notes/new", server.takeRequest().path)
            try { api.setContribution("example", "../notes", "new", false); fail() } catch (_: IllegalArgumentException) { }
        }
    }

    @Test fun previewsCarryEveryMembersDigestInOrderAndOldOnesSendNoBundle() = withHub { server, api ->
        val preview = JsonCodec.decodeFromString<ExtensionPreview>("""{"preview_id":"main","permissions_digest":"d-main",
            "manifest":{"id":"pack","extension_pack":[{"id":"member"}]},
            "bundle":[{"preview_id":"dep","permissions_digest":"d-dep","manifest":{"id":"dep","version":"1.0.0"}},
                {"preview_id":"member","permissions_digest":"d-member","manifest":{"id":"member","version":"2.0.0"},
                    "depends":[{"id":"dep","state":"missing"}]}]}""")
        assertTrue(preview.canConfirm)
        server.enqueue(MockResponse().setBody("""{"id":"pack"}"""))
        api.install(preview)
        val body = JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject
        assertEquals(listOf("dep", "member"), body["bundle"]!!.jsonArray.map { it.jsonObject["preview_id"]!!.jsonPrimitive.content })
        assertEquals(listOf("d-dep", "d-member"), body["bundle"]!!.jsonArray.map { it.jsonObject["permissions_digest"]!!.jsonPrimitive.content })
        server.enqueue(MockResponse().setBody("""{"id":"pack"}"""))
        api.upgrade("pack", ExtensionPreview(previewId = "old", permissionsDigest = "digest"))
        assertFalse("bundle" in JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject)
    }

    @Test fun declarationsReadAndOldHub404MeansNoneNotSuccessOnOtherFailures() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"prompts":[{"extension":"example","extension_name":"示例","id":"today","title":"今天","text":"总结今天"}]}"""))
        assertEquals("总结今天", api.quickPrompts().single().text)
        assertEquals("/api/v1/extensions/-/prompts", server.takeRequest().path)
        server.enqueue(MockResponse().setBody("""{"themes":[{"extension":"example","id":"ocean","name":"海蓝","light":{"accent":"#0059A6"},"dark":{"accent":"#8BC4FF"}}]}"""))
        assertEquals("example/ocean", api.themes().single().key)
        assertEquals("/api/v1/extensions/-/themes", server.takeRequest().path)
        server.enqueue(MockResponse().setBody("""{"templates":[{"extension":"example","id":"quiet","title":"安静","text":"只看提到我的"}]}"""))
        assertEquals("只看提到我的", api.triageTemplates().single().text)
        assertEquals("/api/v1/extensions/-/triage-templates", server.takeRequest().path)
        repeat(3) { server.enqueue(MockResponse().setResponseCode(404)) }
        assertTrue(api.quickPrompts().isEmpty()); assertTrue(api.themes().isEmpty()); assertTrue(api.triageTemplates().isEmpty())
        repeat(3) { server.takeRequest() }
        server.enqueue(MockResponse().setResponseCode(503).setBody("""{"error":"busy"}"""))
        try { api.themes(); fail() } catch (e: HubFailure) { assertEquals(503, e.status) }
    }
}
