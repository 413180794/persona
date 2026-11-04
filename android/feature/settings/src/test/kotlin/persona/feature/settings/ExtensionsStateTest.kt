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

private class FakeExtensions(var detail: ExtensionDetail) : ExtensionApiStub() {
    val configs = mutableListOf<JsonObject>()
    val secrets = mutableListOf<Pair<String, String?>>()
    var list: List<HubExtension> = listOf(detail.extension)
    var refuse: ExtensionConfigRejected? = null
    var fail: Exception? = null

    private fun answer(): ExtensionDetail { fail?.let { throw it }; return detail }
    override suspend fun extensions() = fail?.let { throw it } ?: list
    override suspend fun extension(id: String) = answer()
    override suspend fun setEnabled(id: String, enabled: Boolean): ExtensionDetail {
        detail = detail.copy(extension = detail.extension.copy(enabled = enabled, status = if (enabled) "running" else "disabled"))
        return answer()
    }
    override suspend fun setConfig(id: String, config: JsonObject): ExtensionDetail {
        configs += config
        refuse?.let { throw it }
        detail = detail.copy(config = config)
        return answer()
    }
    override suspend fun setSecret(id: String, name: String, value: String): ExtensionDetail {
        secrets += name to value
        detail = detail.copy(secrets = detail.secrets.map { if (it.name == name) it.copy(set = true) else it })
        return answer()
    }
    override suspend fun clearSecret(id: String, name: String): ExtensionDetail {
        secrets += name to null
        detail = detail.copy(secrets = detail.secrets.map { if (it.name == name) it.copy(set = false) else it })
        return answer()
    }
}

class ExtensionsStateTest {
    private val form = listOf(
        ExtensionField("base_url", "string", title = "服务地址", format = "uri", default = JsonPrimitive("https://notes.example.com"), required = true),
        ExtensionField("folders", "array", items = ExtensionItems("string")),
        ExtensionField("every", "integer", default = JsonPrimitive(30)),
        ExtensionField("ratio", "number"),
        ExtensionField("mode", "string", enum = listOf(ExtensionChoice(JsonPrimitive("fast"), "只要新的"), ExtensionChoice(JsonPrimitive("safe")))),
        ExtensionField("proxy", "object", title = "代理", fields = listOf(ExtensionField("host", "string"), ExtensionField("port", "integer"))),
    )
    private val notes = ExtensionDetail(
        HubExtension("example-notes", "Example Notes", "connectors", "needs_setup", version = "1.2.0", source = "installed", enabled = true),
        form = form, config = buildJsonObject { putJsonArray("folders") { add("工作") } },
        secrets = listOf(ExtensionSecret("api_key", "API Key", required = true)),
    )

    @Test fun wordsForStatesSourcesAndTheSettingsRow() {
        fun p(status: String) = HubExtension("x", "X", "connectors", status)
        assertEquals(listOf("启动", "启动", "停用"), listOf(p("running"), p("stopped"), p("disabled")).map(ExtensionText::state))
        assertEquals(listOf("正在运行", "暂未运行，用到时自动启动", null), listOf(p("running"), p("stopped"), p("error")).map(ExtensionText::runNote))
        assertEquals(Status.Unread, ExtensionText.status(p("stopped")))
        assertEquals("这台 hub 用不了", ExtensionText.state(p("incompatible")))
        assertEquals("状态未知", ExtensionText.state(p("new_state")))
        assertTrue(ExtensionText.isProblem(p("error")) && ExtensionText.isProblem(p("incompatible")))
        assertEquals(Status.NeedsYou, ExtensionText.status(p("needs_setup")))
        assertEquals(Status.Unread, ExtensionText.status(p("running")))
        assertEquals("内置", ExtensionText.source("builtin"))
        assertNull(ExtensionText.source(null))
        assertEquals("连接器", ExtensionText.point("connectors"))
        assertEquals("界面", ExtensionText.point("ui"))
        assertEquals("future_point", ExtensionText.point("future_point"))
        assertEquals("需要设置 · 1.2.0 · 已安装", ExtensionText.stateLine(notes.extension))
        assertNull(ExtensionText.listSummary(null))
        assertEquals("没有", ExtensionText.listSummary(emptyList()))
        assertEquals("3 个", ExtensionText.listSummary(listOf(p("running"), p("disabled"), p("stopped"))))
        assertEquals("3 个 · 2 个要处理", ExtensionText.listSummary(listOf(p("running"), p("needs_setup"), p("error"))))
    }

    @Test fun fieldTitlesPlaceholdersAndSecrets() {
        assertEquals("服务地址", ExtensionText.fieldTitle(form[0]))
        assertEquals("host（必填）", ExtensionText.fieldTitle(ExtensionField("host", required = true)))
        assertEquals("默认：https://notes.example.com", ExtensionText.placeholder(form[0]))
        assertEquals("默认：只要新的", ExtensionText.placeholder(form[4].copy(default = JsonPrimitive("fast"))))
        assertEquals("未设置", ExtensionText.placeholder(form[3]))
        assertEquals("未设置（必填）", ExtensionText.secretState(notes.secrets[0]))
        assertEquals("已设置", ExtensionText.secretState(notes.secrets[0].copy(set = true)))
        assertEquals("未设置", ExtensionText.secretState(ExtensionSecret("x", "X")))
    }

    @Test fun theDraftSetsByPathAndDropsEmptyGroups() {
        var draft = JsonObject(emptyMap())
        draft = ExtensionDraft.set(draft, listOf("proxy", "host"), JsonPrimitive("h"))
        assertEquals(JsonPrimitive("h"), ExtensionDraft.value(draft, listOf("proxy", "host")))
        draft = ExtensionDraft.set(draft, listOf("proxy", "host"), null)
        assertFalse("proxy" in draft)
        assertNull(ExtensionDraft.value(buildJsonObject { put("a", JsonNull) }, listOf("a")))
        val compact = ExtensionDraft.compact(buildJsonObject {
            put("empty", ""); putJsonArray("none") {}; put("keep", "x"); put("zero", 0); put("off", false)
            putJsonObject("group") { put("blank", "") }; putJsonObject("kept") { put("n", 1) }
        })
        assertEquals(buildJsonObject { put("keep", "x"); put("zero", 0); put("off", false); putJsonObject("kept") { put("n", 1) } }, compact)
        assertEquals(setOf("base_url", "folders", "every", "ratio", "mode", "proxy.host", "proxy.port"), ExtensionDraft.paths(form))
    }

    @Test fun numbersAreTypedAsTextUntilTheyReadAsOne() {
        assertEquals(JsonPrimitive(30L) to null, ExtensionDraft.number(" 30 ", integer = true))
        assertEquals(null to "要是一个整数", ExtensionDraft.number("1.5", integer = true))
        assertEquals(JsonPrimitive(1.5) to null, ExtensionDraft.number("1.5", integer = false))
        assertEquals(JsonPrimitive(2L) to null, ExtensionDraft.number("2.0", integer = false))
        assertEquals(null to "要是一个数", ExtensionDraft.number("abc", integer = false))
        assertEquals(null to null, ExtensionDraft.number("  ", integer = true))
    }

    @Test fun editsAreSavedWholeAndCompacted() = runBlocking {
        val api = FakeExtensions(notes)
        val model = ExtensionModel(api, "example-notes")
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
        val model = ExtensionModel(FakeExtensions(notes), "example-notes")
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
        val api = FakeExtensions(notes).apply { refuse = ExtensionConfigRejected("base_url", "要是一个网址") }
        val model = ExtensionModel(api, "example-notes")
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
        api.refuse = ExtensionConfigRejected("", "配置不对")
        assertFalse(model.save())
        assertEquals("没有保存：配置不对", model.state.value.error)
    }

    @Test fun anUnsavedEditSurvivesTheSwitchAndSecrets() = runBlocking {
        val api = FakeExtensions(notes)
        val model = ExtensionModel(api, "example-notes")
        model.load()
        model.edit(listOf("mode"), JsonPrimitive("fast"))
        model.setEnabled(false)
        assertEquals(false, model.state.value.detail?.extension?.enabled)
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
        val api = FakeExtensions(notes).apply { fail = HubFailure(HubError.Server, "扩展不存在", status = 404) }
        val model = ExtensionModel(api, "example-notes")
        model.load()
        assertEquals("读取失败：扩展不存在", model.state.value.error)
        api.fail = null
        model.load()
        assertNull(model.state.value.error)
        api.fail = HubFailure(HubError.Server, "hub 拒绝了", status = 500)
        model.setEnabled(false)
        assertEquals("没有改成：hub 拒绝了", model.state.value.error)
    }

    @Test fun aHubJsonExtensionIsNotEditable() = runBlocking {
        val model = ExtensionModel(FakeExtensions(ExtensionDetail(HubExtension("voice", "语音", "voice", "running", source = "hub.json"), readOnly = true)), "voice")
        model.load()
        assertFalse(model.state.value.editable)
        assertFalse(model.state.value.canSave)
    }

    @Test fun theListLoadsAndReportsFailures() = runBlocking {
        val api = FakeExtensions(notes)
        val list = ExtensionListModel(api)
        list.load()
        assertEquals(listOf("example-notes"), list.state.value.extensions?.map { it.id })
        api.fail = HubFailure(HubError.Server, "坏了", status = 500)
        list.load()
        assertEquals("读取失败：坏了", list.state.value.error)
        assertEquals(1, list.state.value.extensions?.size)
    }

    @Test fun theListKnowsWhetherTheHubHasMarkets() = runBlocking {
        val api = object : ExtensionApiStub() {
            override suspend fun extensions() = listOf(notes.extension)
            override suspend fun listing() = ExtensionListing(extensions(), canInstall = true, markets = true)
        }
        val list = ExtensionListModel(api)
        list.load()
        assertTrue(list.state.value.markets)
        // An older contract (no listing of its own) reads as no markets.
        val old = ExtensionListModel(object : ExtensionApiStub() { override suspend fun extensions() = listOf(notes.extension) })
        old.load()
        assertFalse(old.state.value.markets)
    }
}
