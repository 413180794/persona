package persona.core.hub

import persona.core.contracts.api.PluginConfigRejected
import persona.core.contracts.api.PluginPreview
import persona.core.contracts.api.PluginUiContext
import persona.core.contracts.api.PluginUiUnsupported
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.Assert.*
import org.junit.Test

class HttpPluginManagementTest {
    private fun <T> withHub(block: suspend (MockWebServer, HttpPluginManagement) -> T): T = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            block(server, HttpPluginManagement({ path, method, body ->
                http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
            }))
        }
    }

    private val detail = """{"id":"example-notes","name":"Example Notes","kind":"connectors","status":"needs_setup","version":"1.2.0",
        "source":"installed","enabled":true,"description":"同步笔记","contributes":{"connectors":["examplenotes"]},
        "needs_setup":["密钥「API Key」"],"publisher":"example","homepage":"https://example.com/p","origin":"dir:/opt/p",
        "previous_version":"1.1.0","read_only":false,
        "form":{"fields":[{"name":"base_url","type":"string","title":"服务地址","format":"uri","default":"https://notes.example.com","required":true},
          {"name":"mode","type":"string","enum":[{"value":"fast","title":"只要新的"},{"value":"safe"}]},
          {"name":"tags","type":"array","items":{"type":"string","enum":[{"value":"a","title":"甲"}]},"min_items":1},
          {"name":"proxy","type":"object","title":"代理","fields":[{"name":"port","type":"integer","default":8080,"minimum":1}]}]},
        "config":{"folders":["工作"]},
        "secrets":[{"name":"api_key","title":"API Key","required":true,"set":false}],
        "permissions":[{"key":"runs_code","title":"在 hub 机器上运行程序","detail":"不在沙箱里"}],
        "unknown_key":1}"""

    @Test fun listsThePluginsWithTheNewFieldsAndToleratesAnOlderHub() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"plugins":[
            {"id":"voice","name":"语音","kind":"voice","status":"running"},
            {"id":"example-notes","name":"Example Notes","kind":"connectors","status":"needs_setup","source":"installed",
             "enabled":true,"needs_setup":["密钥「API Key」"],"contributes":{"connectors":["examplenotes"]},"warnings":["w"]}],
            "can_install":true}"""))
        val list = api.plugins()
        assertEquals("/api/v1/plugins", server.takeRequest().path)
        assertEquals(listOf("voice", "example-notes"), list.map { it.id })
        assertNull(list[0].source)
        assertEquals("installed", list[1].source)
        assertEquals(listOf("密钥「API Key」"), list[1].needsSetup)
        assertEquals(mapOf("connectors" to listOf("examplenotes")), list[1].contributes)
        assertEquals(listOf("w"), list[1].warnings)
    }

    @Test fun decodesTheDetailFromOneObject() = withHub { server, api ->
        server.enqueue(MockResponse().setBody(detail))
        val d = api.plugin("example-notes")
        assertEquals("/api/v1/plugins/example-notes/-/", server.takeRequest().path)
        assertEquals("Example Notes", d.plugin.name)
        assertEquals("1.1.0", d.previousVersion)
        assertFalse(d.readOnly)
        assertEquals(listOf("base_url", "mode", "tags", "proxy"), d.form.map { it.name })
        assertEquals(JsonPrimitive("https://notes.example.com"), d.form[0].fallback)
        assertEquals(listOf("只要新的", "safe"), d.form[1].enum!!.map { it.label })
        assertEquals("甲", d.form[2].items!!.enum!!.single().label)
        assertEquals(1, d.form[2].minItems)
        assertEquals(JsonPrimitive(8080), d.form[3].fields!!.single().fallback)
        assertEquals(buildJsonObject { putJsonArray("folders") { add("工作") } }, d.config)
        assertFalse(d.secrets.single().set)
        assertEquals("runs_code", d.permissions.single().key)
    }

    @Test fun aHubJsonPluginHasNoConfigOrSecrets() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"id":"voice","name":"语音","kind":"voice","status":"running","source":"hub.json","read_only":true}"""))
        val d = api.plugin("voice")
        assertTrue(d.readOnly)
        assertTrue(d.form.isEmpty() && d.config.isEmpty() && d.secrets.isEmpty())
    }

    @Test fun switchesSavesAndSetsSecrets() = withHub { server, api ->
        repeat(4) { server.enqueue(MockResponse().setBody(detail)) }
        api.setEnabled("example-notes", false)
        server.takeRequest().let {
            assertEquals("PUT", it.method); assertEquals("/api/v1/plugins/example-notes/-/enabled", it.path)
            assertEquals("""{"enabled":false}""", it.body.readUtf8())
        }
        api.setConfig("example-notes", buildJsonObject { put("base_url", "https://x.example.com") })
        server.takeRequest().let {
            assertEquals("PUT", it.method); assertEquals("/api/v1/plugins/example-notes/-/config", it.path)
            assertEquals("""{"config":{"base_url":"https://x.example.com"}}""", it.body.readUtf8())
        }
        api.setSecret("example-notes", "api_key", "s3cret")
        server.takeRequest().let {
            assertEquals("PUT", it.method); assertEquals("/api/v1/plugins/example-notes/-/secrets/api_key", it.path)
            assertEquals("""{"value":"s3cret"}""", it.body.readUtf8())
        }
        api.clearSecret("example-notes", "api_key")
        server.takeRequest().let { assertEquals("DELETE", it.method); assertEquals("/api/v1/plugins/example-notes/-/secrets/api_key", it.path) }
    }

    @Test fun aRefusedFieldComesBackAsItsPathAndReason() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(400).setBody(
            """{"error":"proxy.host: 要是主机名","code":"invalid_config","field":"proxy.host","message":"要是主机名"}"""))
        val refused = runCatching { api.setConfig("example-notes", JsonObject(emptyMap())) }.exceptionOrNull()
        assertTrue(refused is PluginConfigRejected)
        assertEquals("proxy.host", (refused as PluginConfigRejected).field)
        assertEquals("要是主机名", refused.message)
        server.takeRequest()

        // Another 400 is the hub's error as usual.
        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"bad json"}"""))
        val other = runCatching { api.setConfig("example-notes", JsonObject(emptyMap())) }.exceptionOrNull() as HubFailure
        assertEquals(400, other.status)
        assertEquals("bad json", other.message)
    }

    // OkHttpHubConnection.request throws the hub's error itself instead of returning the response (found on the
    // emulator: the refused field showed as a page error); the field must survive that.
    @Test fun aRefusedFieldSurvivesTheConnectionThrowingFirst() = runBlocking {
        MockWebServer().use { server ->
            val http = OkHttpClient.Builder().retryOnConnectionFailure(false).build()
            val api = HttpPluginManagement({ path, method, body ->
                val response = http.newCall(Request.Builder().url(server.url(path)).method(method, body).build()).execute()
                if (!response.isSuccessful) throw response.use { HubFailure.from(it) }
                response
            })
            server.enqueue(MockResponse().setResponseCode(400).setBody(
                """{"error":"base_url: 要是一个网址","code":"invalid_config","field":"base_url","message":"要是一个网址"}"""))
            val refused = runCatching { api.setConfig("example-notes", JsonObject(emptyMap())) }.exceptionOrNull() as PluginConfigRejected
            assertEquals("base_url", refused.field)
            assertEquals("要是一个网址", refused.message)
        }
    }

    @Test fun refusesIdsAndNamesThatWouldBecomeAnotherPath() = withHub { server, api ->
        assertTrue(runCatching { api.plugin("..") }.exceptionOrNull() is IllegalArgumentException)
        assertTrue(runCatching { api.setSecret("example-notes", "..", "x") }.exceptionOrNull() is IllegalArgumentException)
        assertTrue(runCatching { api.clearSecret("a/b", "api_key") }.exceptionOrNull() is IllegalArgumentException)
        assertEquals(0, server.requestCount)
    }

    @Test fun anUnreadableAnswerIsAProtocolError() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"nope":true}"""))
        assertEquals(HubError.Protocol, (runCatching { api.plugins() }.exceptionOrNull() as HubFailure).kind)
        server.enqueue(MockResponse().setBody("not json"))
        assertEquals(HubError.Protocol, (runCatching { api.plugin("x") }.exceptionOrNull() as HubFailure).kind)
    }

    @Test fun theListSaysWhetherTheHubHasMarkets() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"plugins":[],"can_install":true,"markets":true}"""))
        val listing = api.listing()
        assertTrue(listing.markets && listing.canInstall)
        server.enqueue(MockResponse().setBody("""{"plugins":[]}"""))
        assertFalse(api.listing().markets)
    }

    @Test fun theDetailListsItsPagesOnly() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"id":"example-gateway","name":"网关","status":"running","origin":"market:3f2a9c1d0b/example-gateway",
            "ui":[{"plugin":"example-gateway","id":"home","title":"概况","placement":"plugin_page"},
                  {"plugin":"example-gateway","id":"panel","title":"面板","placement":"launcher_section"}]}"""))
        val d = api.plugin("example-gateway")
        assertEquals(listOf("home"), d.ui.map { it.id })
        assertTrue(d.fromMarket)
    }

    @Test fun asksWherePluginsDrawAndDrawsWithTheContext() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"ui":[{"plugin":"example-gateway","id":"panel","title":"Example 网关","placement":"launcher_section"}]}"""))
        val places = api.uiPlaces("launcher_section", "work key", "workstation")
        assertEquals("/api/v1/plugins/-/ui?placement=launcher_section&launcher=work%20key&machine=workstation", server.takeRequest().path)
        assertEquals("panel", places.single().id)

        server.enqueue(MockResponse().setBody("""{"view":{"title":"T","state":"s1","sections":[{"title":"概况","items":[
            {"type":"metric","label":"余额","value":"¥ 1","tone":"positive"},
            {"type":"progress","label":"今日","ratio":0.72,"tone":"caution"},
            {"type":"row","title":"Key","badge":{"text":"有效","tone":"positive"},"action":{"id":"key:work"}},
            {"type":"button","title":"停用","role":"destructive","action":{"id":"disable","confirm":"确定？"}},
            {"type":"field","name":"plan","label":"套餐","kind":"choice","value":"team","choices":[{"value":"solo","title":"个人"},{"value":"team"}]},
            {"type":"link","title":"后台","url":"https://example.com"},{"type":"divider"},{"type":"text","text":"x","style":"code"},
            {"type":"newer_kind","whatever":1}]}]}}"""))
        val view = api.renderUi("example-gateway", "panel", PluginUiContext("work", "workstation"), state = null)
        server.takeRequest().let {
            assertEquals("POST", it.method)
            assertEquals("/api/v1/plugins/example-gateway/-/ui/panel", it.path)
            assertEquals("""{"context":{"launcher":"work","machine":"workstation"}}""", it.body.readUtf8())
        }
        val items = view.sections.single().items
        assertEquals("s1", view.state)
        assertEquals(0.72, items[1].ratio!!, 0.0)
        assertEquals("有效", items[2].badge!!.text)
        assertEquals("确定？", items[3].action!!.confirm)
        assertEquals(listOf("个人", "team"), items[4].choices!!.map { it.label })
        assertEquals("newer_kind", items.last().type)
    }

    @Test fun pressesAButtonWithTheStateAndValues() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"toast":"已保存","open_url":"https://example.com/k"}"""))
        val result = api.uiAction("example-gateway", "home", "save", state = "tab=overview", values = mapOf("note" to "hi", "auto" to "true"))
        assertEquals("""{"state":"tab=overview","action":"save","values":{"note":"hi","auto":"true"}}""", server.takeRequest().body.readUtf8())
        assertEquals("已保存", result.toast)
        assertEquals("https://example.com/k", result.openUrl)
        assertNull(result.view)
    }

    // #1339: a plugin that says the launcher's gateway is not its own answers 404 unsupported; any other 404 is an error.
    @Test fun anUnsupportedGatewayIsItsOwnFailure() = withHub { server, api ->
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"not my gateway","code":"unsupported"}"""))
        assertTrue(runCatching { api.renderUi("example-gateway", "other", PluginUiContext("work")) }.exceptionOrNull() is PluginUiUnsupported)
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"not my gateway","code":"unsupported"}"""))
        assertTrue(runCatching { api.uiAction("example-gateway", "other", "refresh") }.exceptionOrNull() is PluginUiUnsupported)
        server.enqueue(MockResponse().setResponseCode(404).setBody("""{"error":"no such plugin","code":"not_found"}"""))
        val other = runCatching { api.renderUi("gone", "home") }.exceptionOrNull()
        assertTrue(other is HubFailure && other.status == 404)
        server.enqueue(MockResponse().setResponseCode(502).setBody("""{"error":"插件画的界面这个 hub 画不了","code":"plugin_failed"}"""))
        assertEquals("插件画的界面这个 hub 画不了", (runCatching { api.renderUi("x", "home") }.exceptionOrNull() as HubFailure).message)
    }

    @Test fun marketsAndTheirTokens() = withHub { server, api ->
        val market = """{"id":"3f2a9c1d0b","spec":"github:example/team","name":"团队","has_token":true,"plugins":2,"warnings":["w"],"added_at":"2026-11-02T08:00:00Z"}"""
        server.enqueue(MockResponse().setBody("""{"markets":[$market]}"""))
        val list = api.markets()
        assertEquals("/api/v1/plugins/-/markets", server.takeRequest().path)
        assertTrue(list.single().hasToken)
        assertEquals(listOf("w"), list.single().warnings)

        server.enqueue(MockResponse().setBody(market))
        api.addMarket("github:example/team", "tok")
        server.takeRequest().let {
            assertEquals("POST", it.method); assertEquals("/api/v1/plugins/-/markets", it.path)
            assertEquals("""{"spec":"github:example/team","token":"tok"}""", it.body.readUtf8())
        }
        server.enqueue(MockResponse().setBody(market))
        api.addMarket("https://example.com/p/", "")
        assertEquals("""{"spec":"https://example.com/p/"}""", server.takeRequest().body.readUtf8())
        server.enqueue(MockResponse().setBody(market))
        api.refreshMarket("3f2a9c1d0b")
        server.takeRequest().let { assertEquals("POST", it.method); assertEquals("/api/v1/plugins/-/markets/3f2a9c1d0b/refresh", it.path) }
        server.enqueue(MockResponse().setBody(market))
        api.setMarketToken("3f2a9c1d0b", "")
        server.takeRequest().let {
            assertEquals("PUT", it.method); assertEquals("/api/v1/plugins/-/markets/3f2a9c1d0b/token", it.path)
            assertEquals("""{"token":""}""", it.body.readUtf8())
        }
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        api.removeMarket("3f2a9c1d0b")
        server.takeRequest().let { assertEquals("DELETE", it.method); assertEquals("/api/v1/plugins/-/markets/3f2a9c1d0b", it.path) }

        server.enqueue(MockResponse().setResponseCode(400).setBody("""{"error":"读不到：私有的，要填 token","code":"market_failed"}"""))
        assertEquals("读不到：私有的，要填 token", (runCatching { api.addMarket("github:example/private") }.exceptionOrNull() as HubFailure).message)
        assertTrue(runCatching { api.removeMarket("../x") }.exceptionOrNull() is IllegalArgumentException)
    }

    @Test fun theCatalogAndInstallingFromIt() = withHub { server, api ->
        server.enqueue(MockResponse().setBody("""{"markets":[],"platform":"linux-amd64","plugins":[
            {"market":"m1","market_name":"Example","id":"example-gateway","name":"网关","version":"1.1.0","tags":["额度"],
             "contributes":{"ui":["home"]},"permissions":[{"key":"runs_code","title":"运行程序"}],"installed":"1.0.0",
             "installed_source":"installed","from_this_market":true,"upgradable":true},
            {"market":"m1","id":"w","name":"W","version":"0.2.0","tags":[],"contributes":{},"permissions":[],"problem":"没有包"}]}"""))
        val catalog = api.catalog("网关 面板", "m1", refresh = true)
        assertEquals("/api/v1/plugins/-/catalog?q=%E7%BD%91%E5%85%B3%20%E9%9D%A2%E6%9D%BF&market=m1&refresh=1", server.takeRequest().path)
        assertEquals("linux-amd64", catalog.platform)
        assertTrue(catalog.plugins[0].upgradable && catalog.plugins[0].fromThisMarket)
        assertEquals("没有包", catalog.plugins[1].problem)
        server.enqueue(MockResponse().setBody("""{"markets":[],"plugins":[]}"""))
        api.catalog()
        assertEquals("/api/v1/plugins/-/catalog", server.takeRequest().path)

        server.enqueue(MockResponse().setBody("""{"preview_id":"pv1","manifest":{"id":"example-gateway","name":"网关","version":"1.1.0","contributes":{}},
            "permissions":[{"key":"runs_code","title":"运行程序"}],"permissions_digest":"d1","expires_at":"2026-11-04T07:10:00Z",
            "upgrade":true,"from":"1.0.0","more_permissions":true,"added_permissions":[{"key":"hub_api","title":"完整使用 hub"}],"market":"m1"}"""))
        val preview = api.previewFromMarket("m1", "example-gateway")
        assertEquals("""{"market":"m1","plugin":"example-gateway"}""", server.takeRequest().body.readUtf8())
        assertTrue(preview.upgrade && preview.morePermissions)
        assertEquals("hub_api", preview.addedPermissions!!.single().key)
        assertEquals("1.1.0", preview.manifest!!.version)

        server.enqueue(MockResponse().setBody(detail))
        api.upgrade("example-gateway", preview)
        server.takeRequest().let {
            assertEquals("/api/v1/plugins/example-gateway/-/upgrade", it.path)
            assertEquals("""{"preview_id":"pv1","permissions_digest":"d1"}""", it.body.readUtf8())
        }
        server.enqueue(MockResponse().setBody(detail))
        api.install(PluginPreview("pv2", permissionsDigest = "d2"))
        server.takeRequest().let {
            assertEquals("/api/v1/plugins/-/install", it.path)
            assertEquals("""{"preview_id":"pv2","permissions_digest":"d2"}""", it.body.readUtf8())
        }
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        api.uninstall("example-gateway", keepData = true)
        server.takeRequest().let { assertEquals("DELETE", it.method); assertEquals("/api/v1/plugins/example-gateway/-/?keep_data=1", it.path) }
        server.enqueue(MockResponse().setBody("""{"ok":true}"""))
        api.uninstall("example-gateway")
        assertEquals("/api/v1/plugins/example-gateway/-/", server.takeRequest().path)
    }
}
