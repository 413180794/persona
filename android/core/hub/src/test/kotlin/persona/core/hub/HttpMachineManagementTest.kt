package persona.core.hub

import persona.core.contracts.JsonCodec
import persona.core.contracts.api.*
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.ResponseBody.Companion.toResponseBody
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpMachineManagementTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpMachineManagement) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpMachineManagement { path, method, body, _ ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            })
        }
    }

    @Test fun herdrStatusUsesTheDedicatedGet() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"autostart":true,"running":false,"known":true}"""))
        assertEquals(HerdrState(true, false, true), machines.herdr("desk"))
        server.takeRequest().let {
            assertEquals("/api/v1/machines/desk/herdr", (it.path).zhAny())
            assertEquals("GET", (it.method).zhAny())
        }
    }

    @Test fun herdrStartUsesAnExplicitPostAndKeepsTheAutostartSetting() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"autostart":false,"running":true,"known":true}"""))
        assertEquals(HerdrState(false, true, true), machines.startHerdr("desk/name"))
        server.takeRequest().let {
            assertEquals("/api/v1/machines/desk%2Fname/herdr/start", (it.path).zhAny())
            assertEquals("POST", (it.method).zhAny())
            assertEquals("{}", (it.body.readUtf8()).zhAny())
        }
        server.enqueue(MockResponse().setResponseCode(502).setBody("""{"error":"engine_missing: herdr failed to start"}"""))
        val failed = runCatching { machines.startHerdr("desk") }.exceptionOrNull() as HubFailure
        assertEquals(502, failed.status)
        assertTrue(failed.message.orEmpty().contains("herdr failed to start"))
        assertEquals(2, server.requestCount)
    }

    @Test fun herdrStartAllowsTheHubsFortySecondDeadline() = runBlocking {
        var timeout: Long? = null
        val machines = HttpMachineManagement { _, _, _, seconds ->
            timeout = seconds
            okhttp3.Response.Builder().request(Request.Builder().url("http://localhost/").build())
                .protocol(okhttp3.Protocol.HTTP_1_1).code(200).message("OK")
                .body("""{"autostart":true,"running":true,"known":true}""".toResponseBody()).build()
        }
        machines.startHerdr("desk")
        assertTrue(requireNotNull(timeout) > 40)
    }

    @Test fun launchersReconnectAndDeployGoToTheMachine() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"launchers":[{"name":"claude","model":"opus"},{"name":"codex","agent_type":"codex"}]}"""))
        val launchers = machines.launchers("mac/one")
        assertEquals(listOf("claude", "codex"), (launchers.map { it.name }).zhAny())
        assertEquals("opus", (launchers[0].model).zhAny())
        assertEquals(listOf("claude", "codex"), (launchers.map { it.agentType }).zhAny())
        assertEquals("/api/v1/machines/mac%2Fone/launchers", (server.takeRequest().path).zhAny())

        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        machines.reconnect("mac")
        server.takeRequest().let { assertEquals("POST", (it.method).zhAny()); assertEquals("/api/v1/machines/mac/reconnect", (it.path).zhAny()) }

        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"这台电脑已经是 hub 这一版"}"""))
        val refused = runCatching { machines.deploy("mac") }.exceptionOrNull() as HubFailure
        assertEquals(409, refused.status)
        assertEquals("这台电脑已经是 hub 这一版", (refused.message).zhAny())
        server.takeRequest().let { assertEquals("POST", (it.method).zhAny()); assertEquals("/api/v1/machines/mac/deploy", (it.path).zhAny()) }
    }

    @Test fun readsALaunchersQuotaOnTheMachine() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"machine":"mac","checked_at":"2026-11-04T06:00:00Z","quota":{"gateway":"example-gateway",
            "gateway_name":"示例网关","key_name":"cc_main","budget":[{"label":"每周预算","used_ratio":0.62,"used":496,"limit":800,"unit":"usd",
            "resets_at":"2026-11-06T07:00:00Z"}],"pools":[{"name":"账号 B","state":"cooldown","detail":"冷却中","windows":[{"label":"5 小时",
            "used_ratio":1,"exhausted":true}],"models":[{"model":"example-flash","windows":[{"label":"TPM","used_ratio":0.02}]}]}],
            "usage":[{"label":"今日","requests":287,"tokens":210552323,"cost_usd":123.12}],"a_newer_field":1}}"""))
        val answer = machines.launcherQuota("mac", "cc main")
        assertEquals("/api/v1/launchers/cc%20main/quota?machine=mac", (server.takeRequest().path).zhAny())
        assertEquals("mac", (answer.machine).zhAny())
        val q = answer.quota!!
        assertEquals("示例网关", (q.gatewayName).zhAny())
        assertEquals(800.0, q.budget.single().limit!!, 0.0)
        assertTrue(q.pools.single().windows.single().exhausted)
        assertEquals("example-flash", (q.pools.single().models.single().model).zhAny())
        assertEquals(210552323L, q.usage.single().tokens)

        // ↻ reads again; nothing to read is an answer, not a failure
        server.enqueue(MockResponse().setBody("""{"machine":"mac","checked_at":"2026-11-04T06:00:00Z","error":"这个网关没有能查的额度：没有装能读它的额度扩展","unsupported":true}"""))
        val none = machines.launcherQuota("mac", "plain", refresh = true)
        assertEquals("/api/v1/launchers/plain/quota?machine=mac&refresh=1", (server.takeRequest().path).zhAny())
        assertTrue(none.unsupported)
        assertNull(none.quota)
    }

    @Test fun putsTheEngineAndDecodesTheMachine() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"id":"m1","engine":"orca","engines":[{"id":"orca","name":"Orca","available":true}]}"""))
        val saved = machines.setEngine("m1", "orca")
        val put = server.takeRequest()
        assertEquals("PUT", (put.method).zhAny())
        assertEquals("/api/v1/machines/m1/engine", (put.path).zhAny())
        assertEquals("""{"engine":"orca"}""", (put.body.readUtf8()).zhAny())
        assertEquals("orca", (saved.engine).zhAny())
        assertTrue(saved.engines.single().available)

        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"unknown engine"}"""))
        val unknown = runCatching { machines.setEngine("m1", "nope") }.exceptionOrNull() as HubFailure
        assertEquals(400, unknown.status)
        assertEquals("unknown engine", (unknown.message).zhAny())
        server.takeRequest()
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"no such machine"}"""))
        assertEquals(404, (runCatching { machines.setEngine("x", "herdr") }.exceptionOrNull() as HubFailure).status)
        server.takeRequest()
        // Another machine's state is never taken for this one's.
        server.enqueue(MockResponse().setBody("""{"id":"other","engine":"orca"}"""))
        assertTrue(runCatching { machines.setEngine("m1", "orca") }.exceptionOrNull() is HubFailure)
    }

    @Test fun claudeSettingsWriteOneKeyAndClearingSendsJsonNull() = withHub { server, machines ->
        val table = """{"settings":[{"key":"language","title":"回复语言","type":"string","default":"","value":"中文"}]}"""
        server.enqueue(MockResponse().setBody(table))
        assertEquals("中文", (machines.claudeSettings("mac").single().stringValue).zhAny())
        assertEquals("/api/v1/machines/mac/claude-settings", (server.takeRequest().path).zhAny())

        server.enqueue(MockResponse().setBody(table))
        machines.setClaudeSetting("mac", "language", JsonPrimitive("English"))
        server.takeRequest().let {
            assertEquals("PUT", (it.method).zhAny())
            assertEquals("""{"key":"language","value":"English"}""", (it.body.readUtf8()).zhAny())
        }
        server.enqueue(MockResponse().setBody("""{"settings":[{"key":"language","type":"string","value":null}]}"""))
        val cleared = machines.setClaudeSetting("mac", "language", JsonNull)
        assertEquals("""{"key":"language","value":null}""", (server.takeRequest().body.readUtf8()).zhAny())
        assertTrue(cleared.single().isDefault)
    }

    /** Like [withHub], and keeps each call's read timeout. */
    private fun <T> withTimedHub(block: suspend (MockWebServer, HttpMachineManagement, List<Long?>) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            val timeouts = mutableListOf<Long?>()
            block(server, HttpMachineManagement { path, method, body, timeout ->
                timeouts += timeout
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }, timeouts)
        }
    }

    @Test fun doctorAndItsFixWaitLongerThanTheHubGivesTheNode() = withTimedHub { server, machines, timeouts ->
        server.enqueue(MockResponse().setBody("""{"ok":false,"dir":"~/.persona","checks":[{"name":"node.sock","status":"fail",
            "state":"missing","detail":"no socket","hint":"restart","items":["a"]}],"newer":1}"""))
        val report = machines.doctor("mac/one")
        server.takeRequest().let {
            assertEquals("GET", it.method)
            assertEquals("/api/v1/machines/mac%2Fone/doctor", it.path)
        }
        assertFalse(report.ok)
        assertEquals(DoctorCheck("node.sock", "fail", "missing", "no socket", "restart", listOf("a")), report.checks.single())

        server.enqueue(MockResponse().setBody("""{"ok":true,"checks":[{"name":"node.sock"}],"fixed":["hooks.json","mcp.json"]}"""))
        val fixed = machines.fixDoctor("mac")
        server.takeRequest().let {
            assertEquals("POST", it.method)
            assertEquals("/api/v1/machines/mac/doctor/fix", it.path)
            assertEquals("{}", it.body.readUtf8())
        }
        assertEquals(listOf("hooks.json", "mcp.json"), fixed.fixed)
        // server/internal/hub/node_doctor.go: 45 s to check, 60 s to rewrite and check again.
        assertTrue(requireNotNull(timeouts[0]) > 45)
        assertTrue(requireNotNull(timeouts[1]) > 60)
    }

    @Test fun browserReadsSwitchesAndRunsJobsOnTheMachine() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"enabled":true,"installed":true,"version":"1.2","set_up":true,"imported":true,
            "analytics":"off","ready":true,"screen_sharing":true,"job":{"kind":"open","state":"done"}}"""))
        val read = machines.browser("mac/one")
        server.takeRequest().let { assertEquals("GET", it.method); assertEquals("/api/v1/machines/mac%2Fone/browser", it.path) }
        assertTrue(read.ready && read.setUp && read.imported && read.screenSharing)
        assertEquals("off", read.analytics)
        assertEquals(persona.core.contracts.MachineJob("open", "done"), read.job)

        server.enqueue(MockResponse().setBody("""{"enabled":false}"""))
        assertFalse(machines.setBrowser("mac", false).enabled)
        server.takeRequest().let {
            assertEquals("PUT", it.method)
            assertEquals("/api/v1/machines/mac/browser", it.path)
            assertEquals("""{"enabled":false}""", it.body.readUtf8())
        }

        for (action in listOf("install", "cancel", "open")) {
            server.enqueue(MockResponse().setBody("""{"enabled":true,"job":{"kind":"install","state":"running","step":"download"}}"""))
            assertTrue(requireNotNull(machines.browserJob("mac", action).job).running)
            server.takeRequest().let {
                assertEquals("POST", it.method)
                assertEquals("/api/v1/machines/mac/browser/$action", it.path)
                assertEquals("{}", it.body.readUtf8())
            }
        }
        assertTrue(runCatching { machines.browserJob("mac", "rm") }.exceptionOrNull() is IllegalArgumentException)
        assertEquals("an unknown action never reaches the hub", 5, server.requestCount)
    }

    @Test fun usageWindowsReadsTheAccounts() = withHub { server, machines ->
        server.enqueue(MockResponse().setBody("""{"accounts":[{"id":"a1","provider":"anthropic","name":"Claude","plan":"max",
            "windows":[{"label":"5h","window_minutes":300,"used_percent":42.5,"resets_at":"2026-11-05T12:00:00Z"}],
            "sampled_at":"2026-11-05T09:00:00Z","stale":true}]}"""))
        val account = machines.usageWindows().single()
        server.takeRequest().let { assertEquals("GET", it.method); assertEquals("/api/v1/usage/windows", it.path) }
        assertEquals("a1", account.id)
        assertEquals("max", account.plan)
        assertTrue(account.stale)
        assertEquals(UsageWindow("5h", 300, 42.5, "2026-11-05T12:00:00Z"), account.windows.single())

        server.enqueue(MockResponse().setBody("{}"))
        assertEquals(emptyList<UsageAccount>(), machines.usageWindows())
    }

    @Test fun codexSettingsUseTheirOwnPathAndTheSameBody() = withHub { server, machines ->
        // The shape scripts/mock/mock_hub.py and server/internal/node/codexconfig.go answer with.
        val table = """{"settings":[{"key":"model_reasoning_effort","title":"思考强度","detail":"越高越慢、越仔细","type":"enum",
            "options":["minimal","low","medium","high","xhigh"],"default":"medium","value":"high"},
            {"key":"hide_agent_reasoning","type":"bool","default":false,"value":null}]}"""
        server.enqueue(MockResponse().setBody(table))
        val read = machines.codexSettings("mac")
        assertEquals("/api/v1/machines/mac/codex-settings", (server.takeRequest().path).zhAny())
        assertEquals(listOf("high", ""), (read.map { it.stringValue }).zhAny())
        assertTrue(read[1].isDefault)
        assertEquals(listOf("minimal", "low", "medium", "high", "xhigh"), (read[0].options).zhAny())

        server.enqueue(MockResponse().setBody(table))
        machines.setCodexSetting("mac", "hide_agent_reasoning", JsonPrimitive(true))
        server.takeRequest().let {
            assertEquals("PUT", (it.method).zhAny())
            assertEquals("/api/v1/machines/mac/codex-settings", (it.path).zhAny())
            assertEquals("""{"key":"hide_agent_reasoning","value":true}""", (it.body.readUtf8()).zhAny())
        }
        server.enqueue(MockResponse().setBody(table))
        machines.setCodexSetting("mac", "model", JsonNull)
        assertEquals("""{"key":"model","value":null}""", (server.takeRequest().body.readUtf8()).zhAny())
    }
}

class HttpModelConfigurationTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpModelConfiguration) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpModelConfiguration { path, method, body, _ ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            })
        }
    }

    private val detail = """{"purpose":{"id":"lark.agent","name":"飞书 /agent","kind":"launcher","revision":4,"override":{},
        "effective":{"provider":"strong","model":"main"},"fields":["provider","model","effort"]},"providers":[]}"""

    @Test fun loadsAndPatchesOnePurposeWithNullAndRevision() = withHub { server, models ->
        server.enqueue(MockResponse().setBody("""{"purposes":[],"providers":[]}"""))
        assertTrue(models.catalog().purposes.isEmpty())
        assertEquals("/api/v1/model-config", (server.takeRequest().path).zhAny())
        server.enqueue(MockResponse().setBody(detail))
        val purpose = models.purpose("lark.agent").purpose
        assertEquals("/api/v1/model-config/purposes/lark.agent", (server.takeRequest().path).zhAny())

        server.enqueue(MockResponse().setBody(detail))
        assertTrue(models.save(purpose, ModelConfigurationDraft()) is ModelPurposeSave.Saved)
        val patch = server.takeRequest()
        assertEquals("PATCH", (patch.method).zhAny())
        val body = JsonCodec.parseToJsonElement(patch.body.readUtf8()).jsonObject
        assertEquals(setOf("revision", "provider", "model", "effort"), body.keys)
        assertEquals("4", (body.getValue("revision").jsonPrimitive.content).zhAny())
        assertEquals(JsonNull, body["provider"])

        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":"revision conflict"}"""))
        assertEquals(ModelPurposeSave.Conflict, models.save(purpose, ModelConfigurationDraft(model = "next")))
        assertEquals("next", (JsonCodec.parseToJsonElement(server.takeRequest().body.readUtf8()).jsonObject["model"]?.jsonPrimitive?.content).zhAny())
    }

    @Test fun providerModelsAskTheHubAndOlderHubsHaveNone() = withHub { server, models ->
        server.enqueue(MockResponse().setBody("""{"agent":"claude","machine":"desk","models":[{"id":"opus","name":"Opus","default":true}]}"""))
        val list = models.providerModels("launcher.work", refresh = true)
        assertEquals("/api/v1/model-config/providers/launcher.work/models?refresh=1", (server.takeRequest().path).zhAny())
        assertEquals("desk", (list?.machine).zhAny())
        assertEquals(listOf("opus"), (list?.models?.map { it.id }).zhAny())
        assertTrue(list?.models?.single()?.isDefault == true)
        server.enqueue(MockResponse().setResponseCode(404).setBody("404 page not found"))
        assertNull(models.providerModels("launcher.work"))
        assertEquals("/api/v1/model-config/providers/launcher.work/models", (server.takeRequest().path).zhAny())
        server.enqueue(MockResponse().setBody("""{"agent":"claude","machine":"lab","models":[]}"""))
        assertEquals("lab", (models.providerModels("launcher.work", machine = "lab")?.machine).zhAny())
        assertEquals("/api/v1/model-config/providers/launcher.work/models?machine=lab", (server.takeRequest().path).zhAny())
        server.enqueue(MockResponse().setResponseCode(503).setBody("""{"error":"有这个启动器的电脑都不在线，列不出模型"}"""))
        assertEquals(503, (runCatching { models.providerModels("launcher.work") }.exceptionOrNull() as HubFailure).status)
    }

    @Test fun oldHubsAndMissingPurposesAreToldApart() = withHub { server, models ->
        // An old hub's router: 404 with a plain text body.
        server.enqueue(MockResponse().setResponseCode(404).setBody("404 page not found"))
        assertTrue(runCatching { models.catalog() }.exceptionOrNull() is ModelConfigurationUnsupported)
        server.enqueue(MockResponse().setResponseCode(404).setBody("404 page not found"))
        assertTrue(runCatching { models.purpose("agent.new") }.exceptionOrNull() is ModelConfigurationUnsupported)
        // A new hub without that purpose says so.
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"模型用途不存在"}"""))
        val missing = runCatching { models.purpose("task.gone") }.exceptionOrNull()
        assertTrue(missing is ModelPurposeNotFound)
        assertEquals("模型用途不存在", (missing?.message).zhAny())
        server.enqueue(MockResponse().setResponseCode(500).setBody("""{"error":"boom"}"""))
        assertEquals(500, (runCatching { models.catalog() }.exceptionOrNull() as HubFailure).status)
    }
}
