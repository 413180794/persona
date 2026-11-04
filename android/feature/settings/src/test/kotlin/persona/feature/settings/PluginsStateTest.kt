package persona.feature.settings

import persona.core.contracts.api.*
import persona.core.designsystem.Status
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.*
import org.junit.Assert.*
import org.junit.Test

private class FakePlugins(var detail: PluginDetail) : PluginApiStub() {
    val configs = mutableListOf<JsonObject>()
    val secrets = mutableListOf<Pair<String, String?>>()
    var list: List<HubPlugin> = listOf(detail.plugin)
    var refuse: PluginConfigRejected? = null
    var fail: Exception? = null

    private fun answer(): PluginDetail { fail?.let { throw it }; return detail }
    override suspend fun plugins() = fail?.let { throw it } ?: list
    override suspend fun plugin(id: String) = answer()
    override suspend fun setEnabled(id: String, enabled: Boolean): PluginDetail {
        detail = detail.copy(plugin = detail.plugin.copy(enabled = enabled, status = if (enabled) "running" else "disabled"))
        return answer()
    }
    override suspend fun setConfig(id: String, config: JsonObject): PluginDetail {
        configs += config
        refuse?.let { throw it }
        detail = detail.copy(config = config)
        return answer()
    }
    override suspend fun setSecret(id: String, name: String, value: String): PluginDetail {
        secrets += name to value
        detail = detail.copy(secrets = detail.secrets.map { if (it.name == name) it.copy(set = true) else it })
        return answer()
    }
    override suspend fun clearSecret(id: String, name: String): PluginDetail {
        secrets += name to null
        detail = detail.copy(secrets = detail.secrets.map { if (it.name == name) it.copy(set = false) else it })
        return answer()
    }
}

class PluginsStateTest {
    private val form = listOf(
        PluginField("base_url", "string", title = "服务地址", format = "uri", default = JsonPrimitive("https://notes.example.com"), required = true),
        PluginField("folders", "array", items = PluginItems("string")),
        PluginField("every", "integer", default = JsonPrimitive(30)),
        PluginField("ratio", "number"),
        PluginField("mode", "string", enum = listOf(PluginChoice(JsonPrimitive("fast"), "只要新的"), PluginChoice(JsonPrimitive("safe")))),
        PluginField("proxy", "object", title = "代理", fields = listOf(PluginField("host", "string"), PluginField("port", "integer"))),
    )
    private val notes = PluginDetail(
        HubPlugin("example-notes", "Example Notes", "connectors", "needs_setup", version = "1.2.0", source = "installed", enabled = true),
        form = form, config = buildJsonObject { putJsonArray("folders") { add("工作") } },
        secrets = listOf(PluginSecret("api_key", "API Key", required = true)),
    )

    @Test fun wordsForStatesSourcesAndTheSettingsRow() {
        fun p(status: String) = HubPlugin("x", "X", "connectors", status)
        assertEquals(listOf("启动", "启动", "停用"), listOf(p("running"), p("stopped"), p("disabled")).map(PluginText::state))
        assertEquals(listOf("正在运行", "暂未运行，用到时自动启动", null), listOf(p("running"), p("stopped"), p("error")).map(PluginText::runNote))
        assertEquals(Status.Unread, PluginText.status(p("stopped")))
        assertEquals("这台 hub 用不了", PluginText.state(p("incompatible")))
        assertEquals("状态未知", PluginText.state(p("new_state")))
        assertTrue(PluginText.isProblem(p("error")) && PluginText.isProblem(p("incompatible")))
        assertEquals(Status.NeedsYou, PluginText.status(p("needs_setup")))
        assertEquals(Status.Unread, PluginText.status(p("running")))
        assertEquals("内置", PluginText.source("builtin"))
        assertNull(PluginText.source(null))
        assertEquals("连接器", PluginText.point("connectors"))
        assertEquals("界面", PluginText.point("ui"))
        assertEquals("future_point", PluginText.point("future_point"))
        assertEquals("需要设置 · 1.2.0 · 已安装", PluginText.stateLine(notes.plugin))
        assertNull(PluginText.listSummary(null))
        assertEquals("没有", PluginText.listSummary(emptyList()))
        assertEquals("3 个", PluginText.listSummary(listOf(p("running"), p("disabled"), p("stopped"))))
        assertEquals("3 个 · 2 个要处理", PluginText.listSummary(listOf(p("running"), p("needs_setup"), p("error"))))
    }

    @Test fun fieldTitlesPlaceholdersAndSecrets() {
        assertEquals("服务地址", PluginText.fieldTitle(form[0]))
        assertEquals("host（必填）", PluginText.fieldTitle(PluginField("host", required = true)))
        assertEquals("默认：https://notes.example.com", PluginText.placeholder(form[0]))
        assertEquals("默认：只要新的", PluginText.placeholder(form[4].copy(default = JsonPrimitive("fast"))))
        assertEquals("未设置", PluginText.placeholder(form[3]))
        assertEquals("未设置（必填）", PluginText.secretState(notes.secrets[0]))
        assertEquals("已设置", PluginText.secretState(notes.secrets[0].copy(set = true)))
        assertEquals("未设置", PluginText.secretState(PluginSecret("x", "X")))
    }

    @Test fun theDraftSetsByPathAndDropsEmptyGroups() {
        var draft = JsonObject(emptyMap())
        draft = PluginDraft.set(draft, listOf("proxy", "host"), JsonPrimitive("h"))
        assertEquals(JsonPrimitive("h"), PluginDraft.value(draft, listOf("proxy", "host")))
        draft = PluginDraft.set(draft, listOf("proxy", "host"), null)
        assertFalse("proxy" in draft)
        assertNull(PluginDraft.value(buildJsonObject { put("a", JsonNull) }, listOf("a")))
        val compact = PluginDraft.compact(buildJsonObject {
            put("empty", ""); putJsonArray("none") {}; put("keep", "x"); put("zero", 0); put("off", false)
            putJsonObject("group") { put("blank", "") }; putJsonObject("kept") { put("n", 1) }
        })
        assertEquals(buildJsonObject { put("keep", "x"); put("zero", 0); put("off", false); putJsonObject("kept") { put("n", 1) } }, compact)
        assertEquals(setOf("base_url", "folders", "every", "ratio", "mode", "proxy.host", "proxy.port"), PluginDraft.paths(form))
    }

    @Test fun numbersAreTypedAsTextUntilTheyReadAsOne() {
        assertEquals(JsonPrimitive(30L) to null, PluginDraft.number(" 30 ", integer = true))
        assertEquals(null to "要是一个整数", PluginDraft.number("1.5", integer = true))
        assertEquals(JsonPrimitive(1.5) to null, PluginDraft.number("1.5", integer = false))
        assertEquals(JsonPrimitive(2L) to null, PluginDraft.number("2.0", integer = false))
        assertEquals(null to "要是一个数", PluginDraft.number("abc", integer = false))
        assertEquals(null to null, PluginDraft.number("  ", integer = true))
    }

    @Test fun editsAreSavedWholeAndCompacted() = runBlocking {
        val api = FakePlugins(notes)
        val model = PluginModel(api, "example-notes")
        model.load()
        assertFalse(model.state.value.dirty)
        assertTrue(model.state.value.editable)
        model.edit(listOf("base_url"), JsonPrimitive("https://x.example.com"))
        model.edit(listOf("folders"), null)
        model.edit(listOf("proxy", "host"), JsonPrimitive(""))
        model.editNumber(listOf("every"), "45", integer = true)
        assertTrue(model.state.value.canSave)
        assertTrue(model.save())
        assertEquals(buildJsonObject { put("base_url", "https://x.example.com"); put("every", 45) }, api.configs.single())
        assertFalse(model.state.value.dirty)
        assertTrue(model.state.value.numberText.isEmpty())
    }

    @Test fun aNumberThatIsNotOneKeepsSavingOff() = runBlocking {
        val model = PluginModel(FakePlugins(notes), "example-notes")
        model.load()
        model.editNumber(listOf("proxy", "port"), "80x", integer = true)
        assertEquals("要是一个整数", model.state.value.fieldError(listOf("proxy", "port")))
        assertTrue(model.state.value.dirty)
        assertFalse(model.state.value.canSave)
        assertFalse(model.save())
        model.editNumber(listOf("proxy", "port"), "8080", integer = true)
        assertNull(model.state.value.fieldError(listOf("proxy", "port")))
        assertTrue(model.state.value.canSave)
    }

    @Test fun aRefusedFieldShowsBesideItAndTheDraftStays() = runBlocking {
        val api = FakePlugins(notes).apply { refuse = PluginConfigRejected("base_url", "要是一个网址") }
        val model = PluginModel(api, "example-notes")
        model.load()
        model.edit(listOf("base_url"), JsonPrimitive("not a url"))
        assertFalse(model.save())
        assertEquals("要是一个网址", model.state.value.fieldError(listOf("base_url")))
        assertEquals(JsonPrimitive("not a url"), model.state.value.value(listOf("base_url")))
        assertNull(model.state.value.error)
        // Editing that field clears the hub's word on it.
        model.edit(listOf("base_url"), JsonPrimitive("https://ok.example.com"))
        assertNull(model.state.value.fieldError(listOf("base_url")))

        // A field the form does not draw goes on top instead.
        api.refuse = PluginConfigRejected("", "配置不对")
        assertFalse(model.save())
        assertEquals("没有保存：配置不对", model.state.value.error)
    }

    @Test fun anUnsavedEditSurvivesTheSwitchAndSecrets() = runBlocking {
        val api = FakePlugins(notes)
        val model = PluginModel(api, "example-notes")
        model.load()
        model.edit(listOf("mode"), JsonPrimitive("fast"))
        model.setEnabled(false)
        assertEquals(false, model.state.value.detail?.plugin?.enabled)
        assertEquals(JsonPrimitive("fast"), model.state.value.value(listOf("mode")))
        assertTrue(model.setSecret("api_key", "  s3cret \n"))
        assertEquals("api_key" to "s3cret", api.secrets.single())
        assertTrue(model.state.value.detail!!.secrets.single().set)
        assertFalse(model.setSecret("api_key", "   "))
        assertTrue(model.clearSecret("api_key"))
        assertFalse(model.state.value.detail!!.secrets.single().set)
        assertEquals(JsonPrimitive("fast"), model.state.value.value(listOf("mode")))
    }

    @Test fun failuresSayWhatDidNotHappen() = runBlocking {
        val api = FakePlugins(notes).apply { fail = HubFailure(HubError.Server, "插件不存在", status = 404) }
        val model = PluginModel(api, "example-notes")
        model.load()
        assertEquals("读取失败：插件不存在", model.state.value.error)
        api.fail = null
        model.load()
        assertNull(model.state.value.error)
        api.fail = HubFailure(HubError.Server, "hub 拒绝了", status = 500)
        model.setEnabled(false)
        assertEquals("没有改成：hub 拒绝了", model.state.value.error)
    }

    @Test fun aHubJsonPluginIsNotEditable() = runBlocking {
        val model = PluginModel(FakePlugins(PluginDetail(HubPlugin("voice", "语音", "voice", "running", source = "hub.json"), readOnly = true)), "voice")
        model.load()
        assertFalse(model.state.value.editable)
        assertFalse(model.state.value.canSave)
    }

    @Test fun theListLoadsAndReportsFailures() = runBlocking {
        val api = FakePlugins(notes)
        val list = PluginListModel(api)
        list.load()
        assertEquals(listOf("example-notes"), list.state.value.plugins?.map { it.id })
        api.fail = HubFailure(HubError.Server, "坏了", status = 500)
        list.load()
        assertEquals("读取失败：坏了", list.state.value.error)
        assertEquals(1, list.state.value.plugins?.size)
    }

    @Test fun theListKnowsWhetherTheHubHasMarkets() = runBlocking {
        val api = object : PluginApiStub() {
            override suspend fun plugins() = listOf(notes.plugin)
            override suspend fun listing() = PluginListing(plugins(), canInstall = true, markets = true)
        }
        val list = PluginListModel(api)
        list.load()
        assertTrue(list.state.value.markets)
        // An older contract (no listing of its own) reads as no markets.
        val old = PluginListModel(object : PluginApiStub() { override suspend fun plugins() = listOf(notes.plugin) })
        old.load()
        assertFalse(old.state.value.markets)
    }
}
