package jarvis.android

import jarvis.android.data.ApiClient
import jarvis.android.data.HubException
import jarvis.android.data.JsonCodec
import jarvis.android.settings.ModelBinding
import jarvis.android.settings.ModelConfigurationBody
import jarvis.android.settings.ModelConfigurationDraft
import jarvis.android.settings.ModelProvider
import jarvis.android.settings.ModelPurpose
import jarvis.android.settings.modelConfiguration
import jarvis.android.settings.modelPurpose
import jarvis.android.settings.modelPurposeGroups
import jarvis.android.settings.saveModelPurpose
import jarvis.android.voice.startFrame
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.TimeUnit

class ModelConfigurationTest {
    private val purpose = ModelPurpose(
        id = "lark.agent", name = "飞书 /agent", group = "对话", kind = "launcher", revision = 4,
        override = ModelBinding("strong", "main", "high"), effective = ModelBinding("strong", "main", "high"),
        fields = listOf("provider", "model", "effort"),
    )

    @Test fun emptyFieldsRestoreInheritanceAndUnsupportedFieldsStayAbsent() {
        val patch = JsonCodec.parseToJsonElement(ModelConfigurationDraft(provider = " other ").patch(purpose)) as JsonObject
        assertEquals("4", patch["revision"]!!.jsonPrimitive.content)
        assertEquals("other", patch["provider"]!!.jsonPrimitive.content)
        assertEquals(JsonNull, patch["model"])
        assertEquals(JsonNull, patch["effort"])
        val modelOnly = JsonCodec.parseToJsonElement(ModelConfigurationDraft(model = "next").patch(purpose.copy(fields = listOf("model")))) as JsonObject
        assertEquals(setOf("revision", "model"), modelOnly.keys)
    }

    @Test fun choosingAnotherProviderClearsModelAndEffort() {
        val draft = ModelConfigurationDraft.from(purpose)
        assertEquals(ModelConfigurationDraft("strong", "main", "high"), draft)
        assertEquals(ModelConfigurationDraft(provider = "other"), draft.choosingProvider("other"))
        assertEquals(draft, draft.choosingProvider("strong"))
        assertEquals(ModelConfigurationDraft(), ModelConfigurationDraft.from(purpose.copy(override = ModelBinding())))
    }

    @Test fun candidatesUseHubCapabilitiesAndRetainReferencedHiddenProviders() {
        val providers = listOf(
            ModelProvider("strong", "常用", hidden = true), ModelProvider("other", "其他"),
            ModelProvider("hidden", "隐藏", hidden = true), ModelProvider("api", "API", kind = "api"),
        )
        assertEquals(listOf("strong", "other"), purpose.providersFrom(providers).map { it.id })
        assertEquals(listOf("api"), purpose.copy(providerIds = listOf("api")).providersFrom(providers).map { it.id })
        assertTrue(purpose.copy(providerIds = emptyList()).providersFrom(providers).isEmpty())
        assertEquals("常用 · main · 思考 high", purpose.summary(providers))
    }

    @Test fun groupsSearchRealPurposesAndRoutesKeepIds() {
        val triage = purpose.copy(id = "inbox.triage", name = "分拣", group = "收件箱")
        assertEquals(mapOf("收件箱" to listOf(triage)), modelPurposeGroups(listOf(purpose, triage), "分拣"))
        assertTrue(modelPurposeGroups(listOf(purpose), "技能").isEmpty())
        assertEquals(jarvis.android.ui.settings.SettingsRoute.Models, jarvis.android.ui.settings.SettingsRoute.of("model-config"))
        assertEquals(jarvis.android.ui.settings.SettingsRoute.ModelPurpose("plugin.voice"), jarvis.android.ui.settings.SettingsRoute.of("model-purpose/plugin.voice"))
    }

    @Test fun decodesTheCatalogAndSendsTheSelectedPluginModel() {
        val body = JsonCodec.decodeFromString<ModelConfigurationBody>("""{"purposes":[{"id":"plugin.voice","kind":"plugin","effective":{"provider":"plugin.voice","model":"voice-next"},"override":{},"revision":2,"fields":["model"],"future":true}],"providers":[]}""")
        val p = body.purposes.single()
        assertEquals("plugin", p.kind)
        assertEquals("""{"type":"start","model":"voice-next"}""", startFrame(null, p.effective.model))
    }

    @Test fun realTransportLoadsAndPatchesOnePurposeWithNullAndRevision() = runBlocking {
        val server = MockWebServer()
        server.start()
        try {
            val client = ApiClient(server.url("/").toString(), "mock")
            val detail = """{"purpose":{"id":"lark.agent","name":"飞书 /agent","kind":"launcher","revision":4,"override":{},"effective":{"provider":"strong","model":"main"},"fields":["provider","model","effort"]},"providers":[]}"""
            server.enqueue(MockResponse().setBody("""{"purposes":[],"providers":[]}"""))
            assertTrue(client.modelConfiguration().purposes.isEmpty())
            assertEquals("/api/v1/model-config", server.takeRequest(2, TimeUnit.SECONDS)!!.path)
            server.enqueue(MockResponse().setBody(detail))
            val p = client.modelPurpose("lark.agent").purpose
            assertEquals("/api/v1/model-config/purposes/lark.agent", server.takeRequest(2, TimeUnit.SECONDS)!!.path)
            server.enqueue(MockResponse().setBody(detail))
            client.saveModelPurpose(p, ModelConfigurationDraft())
            val req = server.takeRequest(2, TimeUnit.SECONDS)!!
            assertEquals("PATCH", req.method)
            assertEquals("Bearer mock", req.getHeader("Authorization"))
            val json = JsonCodec.parseToJsonElement(req.body.readUtf8()) as JsonObject
            assertEquals("4", json["revision"]!!.jsonPrimitive.content)
            assertEquals(JsonNull, json["provider"])
            assertEquals(JsonNull, json["model"])
            assertEquals(JsonNull, json["effort"])
            assertFalse(json.keys.any { "key" in it })
            server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"revision conflict"}"""))
            try {
                client.saveModelPurpose(p, ModelConfigurationDraft(model = "next"))
                throw AssertionError("expected a conflict")
            } catch (e: HubException) {
                assertEquals(409, e.status)
            }
        } finally {
            server.shutdown()
        }
    }
}
