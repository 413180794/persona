package persona.feature.settings

import persona.core.contracts.api.*
import persona.core.designsystem.Tone
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import persona.core.designsystem.JChartKind
import org.junit.Assert.*
import org.junit.Test

private class FakeUi : PluginApiStub() {
    var view = PluginView(title = "Example 网关", state = "tab=1", sections = listOf(PluginViewSection(items = listOf(
        PluginViewItem("field", name = "note", kind = "text", value = "旧的"),
        PluginViewItem("field", name = "token", kind = "secret", value = "plugin must not prefill"),
        PluginViewItem("field", name = "auto", kind = "toggle", value = "true"),
        PluginViewItem("field", name = "plan", kind = "choice", value = "team", choices = listOf(PluginViewChoice("solo"), PluginViewChoice("team", "团队"))),
        PluginViewItem("button", title = "保存", action = PluginViewAction("save", submit = true)),
    ))))
    var answer = PluginUiResult()
    var fail: Exception? = null
    val renders = mutableListOf<Pair<PluginUiContext?, String?>>()
    val actions = mutableListOf<Triple<String, String?, Map<String, String>?>>()
    var places = listOf(PluginUiPlace("example-gateway", "panel", "Example 网关", "launcher_section"))
    var placeContext: Pair<String?, String?>? = null

    override suspend fun renderUi(plugin: String, ui: String, context: PluginUiContext?, state: String?): PluginView {
        renders += context to state
        fail?.let { throw it }
        return view
    }
    override suspend fun uiAction(plugin: String, ui: String, action: String, context: PluginUiContext?, state: String?,
        values: Map<String, String>?): PluginUiResult {
        actions += Triple(action, state, values)
        fail?.let { throw it }
        return answer
    }
    override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?): List<PluginUiPlace> {
        placeContext = launcher to machine
        fail?.let { throw it }
        return places
    }
}

class PluginUiStateTest {
    @Test fun tonesLinksAndItemsTheAppDraws() {
        assertEquals(Tone.Positive, PluginViewText.tone("positive"))
        assertEquals(Tone.Danger, PluginViewText.tone("danger"))
        assertEquals(Tone.Caution, PluginViewText.tone("caution"))
        assertEquals(Tone.Neutral, PluginViewText.tone("purple"))
        assertEquals(Tone.Neutral, PluginViewText.tone(null))
        assertTrue(PluginViewText.isHttps("https://example.com/console"))
        assertFalse(PluginViewText.isHttps("http://example.com"))
        assertFalse(PluginViewText.isHttps("https://"))
        assertFalse(PluginViewText.isHttps("javascript:alert(1)"))
        assertTrue(PluginViewText.drawable(PluginViewItem("link", title = "后台", url = "https://example.com")))
        assertFalse(PluginViewText.drawable(PluginViewItem("link", title = "后台", url = "http://example.com")))
        assertFalse(PluginViewText.drawable(PluginViewItem("chart")))
        assertFalse(PluginViewText.drawable(PluginViewItem("button", title = "无动作")))
        assertFalse(PluginViewText.drawable(PluginViewItem("field", name = "x", kind = "date")))
        listOf("text", "secret", "number", "toggle", "choice").forEach {
            assertTrue(it, PluginViewText.drawable(PluginViewItem("field", name = "x", kind = it)))
        }
        assertTrue(PluginViewText.grouped(PluginViewItem("row")))
        assertTrue(PluginViewText.grouped(PluginViewItem("field", kind = "choice")))
        assertFalse(PluginViewText.grouped(PluginViewItem("field", kind = "text")))
        assertFalse(PluginViewText.grouped(PluginViewItem("button")))
        assertEquals(1f, PluginViewText.ratio(PluginViewItem("progress", ratio = 1.4)))
        assertEquals(0f, PluginViewText.ratio(PluginViewItem("progress", ratio = -1.0)))
        assertEquals("72%", PluginViewText.percent(PluginViewItem("progress", ratio = 0.72)))
        assertTrue(PluginViewText.isNumber("-12.5") && PluginViewText.isNumber(""))
        assertFalse(PluginViewText.isNumber("12a"))
        assertEquals("gw.example.com", PluginViewText.host("https://gw.example.com/v1"))
        assertNull(PluginViewText.host(""))
        assertEquals("停用测试 Key", PluginViewText.confirmLabel(PluginViewItem("button", title = "停用测试 Key")))
        assertEquals("确定", PluginViewText.confirmLabel(PluginViewItem("row", title = "一个很长很长很长的按钮名字")))
    }

    @Test fun drawsWithTheFieldsAsSentButNeverASecret() = runBlocking {
        val api = FakeUi()
        val model = PluginViewModel(api, "example-gateway", "home")
        model.load()
        val s = model.state.value
        assertEquals("Example 网关", s.view?.title)
        assertEquals(mapOf("note" to "旧的", "token" to "", "auto" to "true", "plan" to "team"), s.values)
        assertNull(api.renders.single().second)
        // Drawing again sends the plugin's state back.
        model.load()
        assertEquals("tab=1", api.renders.last().second)
    }

    @Test fun aSubmittingButtonSendsTheFieldsAndANewViewReplacesThePage() = runBlocking {
        val api = FakeUi()
        val model = PluginViewModel(api, "example-gateway", "home")
        model.load()
        model.edit("note", "新的")
        model.edit("token", "s3cret")
        model.edit("auto", "false")
        api.answer = PluginUiResult(view = PluginView(title = "已保存", sections = emptyList()), toast = "已保存")
        val out = model.press(PluginViewAction("save", submit = true))
        val (action, state, values) = api.actions.single()
        assertEquals("save", action)
        assertEquals("tab=1", state)
        assertEquals(mapOf("note" to "新的", "token" to "s3cret", "auto" to "false", "plan" to "team"), values)
        assertEquals("已保存", out.toast)
        assertEquals("已保存", model.state.value.view?.title)
        assertNull(model.state.value.acting)
    }

    @Test fun aPlainButtonSendsNoValuesAndNothingBackDrawsAgain() = runBlocking {
        val api = FakeUi()
        val model = PluginViewModel(api, "example-gateway", "home")
        model.load()
        val out = model.press(PluginViewAction("refresh"))
        assertNull(api.actions.single().third)
        assertEquals(PluginActionOutcome(), out)
        assertEquals(2, api.renders.size)
    }

    @Test fun aLinkBackMustBeHttpsAndAToastDoesNotRedraw() = runBlocking {
        val api = FakeUi()
        val model = PluginViewModel(api, "example-gateway", "home")
        model.load()
        api.answer = PluginUiResult(openUrl = "https://example.com/keys")
        assertEquals("https://example.com/keys", model.press(PluginViewAction("key")).openUrl)
        api.answer = PluginUiResult(openUrl = "http://example.com/keys", toast = "好")
        val out = model.press(PluginViewAction("key"))
        assertNull(out.openUrl)
        assertEquals("好", out.toast)
        assertEquals(1, api.renders.size)
    }

    @Test fun failuresSayWhyAndKeepTheView() = runBlocking {
        val api = FakeUi()
        val model = PluginViewModel(api, "example-gateway", "home")
        api.fail = HubFailure(HubError.Server, "插件出错了", status = 502, code = "plugin_failed")
        model.load()
        assertEquals("读取失败：插件出错了", model.state.value.error)
        assertNull(model.state.value.view)
        assertFalse(model.state.value.unsupported)
        api.fail = null
        model.load()
        assertNull(model.state.value.error)
        api.fail = HubFailure(HubError.Server, "超时", status = 502)
        val out = model.press(PluginViewAction("save"))
        assertEquals("没有做成：超时", out.error)
        assertNotNull(model.state.value.view)
    }

    // #1339: a launcher section without hosts is listed on every launcher; a plugin that says the gateway is not its
    // own answers 404 unsupported, and the section is hidden: no error, nothing drawn.
    @Test fun anUnsupportedGatewayHidesTheSectionWithoutAnError() = runBlocking {
        val api = FakeUi()
        val context = PluginUiContext("work", "workstation")
        val model = PluginViewModel(api, "example-gateway", "other", context)
        api.fail = PluginUiUnsupported()
        model.load()
        val s = model.state.value
        assertTrue(s.unsupported)
        assertNull(s.error)
        assertNull(s.view)
        assertEquals(context, api.renders.single().first)
        // Answered by a button too (the plugin changed its mind): hidden, no error.
        api.fail = null
        model.load()
        assertFalse(model.state.value.unsupported)
        api.fail = PluginUiUnsupported()
        assertEquals(PluginActionOutcome(), model.press(PluginViewAction("refresh")))
        assertTrue(model.state.value.unsupported)
    }

    @Test fun theLaunchersPlacesAskWithTheLauncherAndMachine() = runBlocking {
        val api = FakeUi()
        api.places = api.places + PluginUiPlace("other", "page", "别的", "plugin_page")
        val places = PluginPlacesModel(api, "launcher_section", PluginUiContext("work", "workstation"))
        places.load()
        assertEquals("work" to "workstation", api.placeContext)
        assertEquals(listOf("panel"), places.state.value.places?.map { it.id })
        api.fail = HubFailure(HubError.Server, "坏了", status = 500)
        places.load()
        assertEquals("插件的内容读不到：坏了", places.state.value.error)
        assertEquals(1, places.state.value.places?.size)
    }

    // UI version 2 (docs/plugins.md「UI 插槽」「新条目」): charts, number grids and segmented controls draw; a kind this
    // app does not know, or one it cannot draw, is skipped and the rest of the view stays.
    @Test fun theNewItemsDrawAndUnknownOnesAreSkipped() {
        val chart = PluginViewItem("chart", kind = "bar", unit = "tokens", stacked = true, series = listOf(
            PluginChartSeries("输入", points = listOf(PluginChartPoint("11-01", 1_200_000.0, "1.2M"), PluginChartPoint("11-02", 800_000.0))),
            PluginChartSeries("输出", "positive", listOf(PluginChartPoint("11-01", 3.0), PluginChartPoint("11-02", 4.5), PluginChartPoint("11-03", 9.0))),
            PluginChartSeries("别的", "purple", listOf(PluginChartPoint("11-01", 1.0), PluginChartPoint("11-02", 2.0)))))
        assertTrue(PluginViewText.drawable(chart))
        val data = PluginViewText.chart(chart)!!
        assertEquals(JChartKind.Bar, data.kind)
        assertTrue(data.stacked)
        assertEquals(listOf("11-01", "11-02"), data.labels)
        assertEquals(listOf("1.2M", "800K tokens"), data.series[0].displays)
        // Every series has as many points as the labels; a tone is a role colour, an unknown one neutral, none the series colour.
        assertEquals(listOf(3.0, 4.5), data.series[1].values)
        assertEquals(Tone.Positive, data.series[1].tone)
        assertEquals(Tone.Neutral, data.series[2].tone)
        assertNull(data.series[0].tone)
        assertEquals("3.5 元", PluginViewText.display(3.5, null, "元"))
        assertEquals("¥1.20", PluginViewText.display(1.2, "¥1.20", "元"))
        assertNull(PluginViewText.chart(chart.copy(kind = "pie")))
        assertFalse(PluginViewText.drawable(chart.copy(kind = "pie")))
        assertFalse(PluginViewText.drawable(chart.copy(series = listOf(PluginChartSeries("空")))))
        assertEquals(JChartKind.Line, PluginViewText.chart(chart.copy(kind = "line"))!!.kind)

        val grid = PluginViewItem("stat_grid", stats = listOf(PluginStat("今天", "1.2M", "+12%", "caution"), PluginStat("费用", "¥36.20", tone = "lilac")))
        assertTrue(PluginViewText.drawable(grid))
        assertFalse(PluginViewText.drawable(PluginViewItem("stat_grid")))
        assertEquals(listOf(Tone.Caution, Tone.Neutral), PluginViewText.stats(grid).map { it.tone })

        val segmented = PluginViewItem("segmented", name = "range", value = "7d", action = PluginViewAction("range"),
            choices = listOf(PluginViewChoice("1d", "今天"), PluginViewChoice("7d")))
        assertTrue(PluginViewText.drawable(segmented))
        assertFalse(PluginViewText.drawable(segmented.copy(choices = segmented.choices!!.take(1))))
        assertFalse(PluginViewText.drawable(segmented.copy(action = null)))

        assertFalse(PluginViewText.drawable(PluginViewItem("calendar_heatmap")))
        // A card draws no fields (it opens a page for that).
        assertFalse(PluginViewText.drawableOnCard(PluginViewItem("field", name = "x", kind = "text")))
        assertTrue(PluginViewText.drawableOnCard(grid))
    }

    private val range = PluginViewItem("segmented", name = "range", value = "7d", action = PluginViewAction("range"),
        choices = listOf(PluginViewChoice("1d", "今天"), PluginViewChoice("7d", "7 天"), PluginViewChoice("30d", "30 天")))

    private fun rangeView(value: String, state: String = "range=$value") = PluginView(title = "用量", state = state, sections = listOf(
        PluginViewSection(items = listOf(range.copy(value = value), PluginViewItem("field", name = "note", kind = "text", value = "备注")))))

    // Choosing another segment sends its action at once with {name: value}; the page shows the choice right away and
    // the plugin's new view replaces it. The same choice sends nothing; a failure puts the old choice back.
    @Test fun aSegmentedChoiceGoesAtOnce() = runBlocking {
        val api = FakeUi()
        api.view = rangeView("7d")
        val model = PluginViewModel(api, "example-gateway", "usage")
        model.load()
        assertEquals("7d", model.state.value.values["range"])
        api.answer = PluginUiResult(view = rangeView("30d"))
        model.choose(range, "30d")
        assertEquals(Triple("range", "range=7d", mapOf("range" to "30d")), api.actions.single())
        assertEquals("range=30d", model.state.value.view?.state)
        assertEquals("30d", model.state.value.values["range"])
        model.choose(range, "30d")
        assertEquals(1, api.actions.size)
        api.fail = HubFailure(HubError.Server, "坏了", status = 502)
        val out = model.choose(range, "1d")
        assertEquals("没有做成：坏了", out.error)
        assertEquals("30d", model.state.value.values["range"])
        // Submitting: the page's fields go with it.
        api.fail = null
        api.answer = PluginUiResult(view = rangeView("1d"))
        model.choose(range.copy(action = PluginViewAction("range", submit = true)), "1d")
        assertEquals(mapOf("range" to "1d", "note" to "备注"), api.actions.last().third)
    }

    // refresh_after (docs/plugins.md「UI 插槽」「刷新」): drawn again with the plugin's state once the time is up (30 s at
    // least) while the page runs it; stopped, nothing; run again after the time is up, drawn at once.
    @Test fun refreshAfterDrawsAgainWhileOnScreen() = runTest {
        val api = FakeUi()
        api.view = api.view.copy(refreshAfter = 10)
        val model = PluginViewModel(api, "example-gateway", "usage", now = { testScheduler.currentTime })
        model.load()
        val fresh = launch { model.keepFresh() }
        advanceTimeBy(29_000); runCurrent()
        assertEquals(1, api.renders.size)
        advanceTimeBy(1_500); runCurrent()
        assertEquals(2, api.renders.size)
        assertEquals("tab=1", api.renders.last().second)
        advanceTimeBy(30_500); runCurrent()
        assertEquals(3, api.renders.size)
        fresh.cancel()
        advanceTimeBy(120_000); runCurrent()
        assertEquals(3, api.renders.size)
        val back = launch { model.keepFresh() }
        runCurrent()
        assertEquals(4, api.renders.size)
        back.cancel()
    }

    @Test fun aViewWithoutRefreshAfterIsNotDrawnAgain() = runTest {
        val api = FakeUi()
        val model = PluginViewModel(api, "example-gateway", "usage", now = { testScheduler.currentTime })
        model.load()
        val fresh = launch { model.keepFresh() }
        advanceTimeBy(48L * 3600 * 1000); runCurrent()
        assertEquals(1, api.renders.size)
        fresh.cancel()
        // A day at most.
        assertEquals(1_000L + 86_400_000L, PluginViewText.refreshAt(PluginViewState(PluginView(refreshAfter = 1_000_000), drawnAt = 1_000L)))
        assertNull(PluginViewText.refreshAt(PluginViewState(PluginView(refreshAfter = 0), drawnAt = 1_000L)))
    }

    // A machine's section for a machine the hub does not know (another hub's) is hidden, not an error.
    @Test fun aMachineTheHubDoesNotKnowHidesItsSection() = runBlocking {
        val api = FakeUi()
        api.fail = PluginUiNotFound("没有这台机器")
        val model = PluginViewModel(api, "example-gateway", "gpu", PluginUiContext(machine = "elsewhere"))
        model.load()
        assertTrue(model.state.value.unsupported)
        assertNull(model.state.value.error)
    }
}
