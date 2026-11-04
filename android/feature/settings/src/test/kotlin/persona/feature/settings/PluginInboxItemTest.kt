package persona.feature.settings

import persona.core.contracts.api.*
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.core.navigation.InboxItemUiRoute
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.*
import org.junit.Test

// An inbox item's page drawn by its connector's plugin (docs/plugins.md「UI 插槽」inbox_item, #1683): which pages the
// hub lists, drawing and pressing with the item in the context, and when the item's own page opens instead.
class PluginInboxItemTest {
    private val event = PluginUiPlace("lark-connector", "event", "事件", "inbox_item", pluginName = "飞书", connector = "lark")
    private val route = InboxItemUiRoute("lark:F1:1051", "lark-connector", "event", "周四的评审")

    private class FakeHub(val byPlacement: MutableMap<String, List<PluginUiPlace>>) : PluginApiStub() {
        val asked = mutableListOf<String>()
        val failing = mutableMapOf<String, Exception>()
        override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?): List<PluginUiPlace> {
            asked += placement
            failing[placement]?.let { throw it }
            return byPlacement[placement].orEmpty()
        }
    }

    @Test fun readsTheInboxItemPagesByTheirPlacement() = runTest {
        val hub = FakeHub(mutableMapOf(
            "" to listOf(PluginUiPlace("lark-connector", "accounts", "账号", "nav_page")),
            // One without a connector draws for nobody's items; another slot listed here is not an item's page.
            "inbox_item" to listOf(event, event.copy(id = "loose", connector = null), event.copy(id = "card", placement = "overview_card")),
        ))
        val slotsOn = MutableStateFlow(true)
        val slots = PluginSlots(hub, slotsOn, MutableSharedFlow())
        assertFalse("not known before it is read", slots.state.value.inboxItemsKnown)
        backgroundScope.launch { slots.run(MutableStateFlow(true)) }
        runCurrent()
        assertEquals(listOf("", "inbox_item"), hub.asked)
        assertEquals(listOf(event), slots.state.value.inboxItems)
        assertTrue(slots.state.value.inboxItemsKnown)
        assertEquals(listOf("accounts"), slots.state.value.navPages.map { it.id })

        // The hub cannot answer: the pages known so far stay, nothing to tell the user.
        hub.failing["inbox_item"] = HubFailure(HubError.Server, "坏了", status = 500)
        slots.load()
        assertEquals(listOf(event), slots.state.value.inboxItems)
        assertNull(slots.state.value.error)

        // An older hub does not know the slot and lists nothing there; nor does a failing list of the other slots
        // stop the inbox items' pages from being read.
        hub.failing.clear()
        hub.failing[""] = HubFailure(HubError.Server, "坏了", status = 500)
        hub.byPlacement["inbox_item"] = emptyList()
        slots.load()
        assertTrue(slots.state.value.inboxItems.isEmpty())
        assertEquals("插件的入口读不到：坏了", slots.state.value.error)

        // The inbox shows: only the inbox items' pages are read again.
        hub.failing.clear()
        hub.byPlacement["inbox_item"] = listOf(event)
        hub.asked.clear()
        slots.loadInboxItems()
        assertEquals(listOf("inbox_item"), hub.asked)
        assertEquals(listOf(event), slots.state.value.inboxItems)

        // Another hub without slots selected: nothing known about its items any more.
        slotsOn.value = false
        runCurrent()
        assertFalse(slots.state.value.inboxItemsKnown)
        assertTrue(slots.state.value.inboxItems.isEmpty())
    }

    @Test fun aFailedFirstReadLeavesTheItemPagesUnknown() = runTest {
        val hub = FakeHub(mutableMapOf("inbox_item" to listOf(event)))
        hub.failing["inbox_item"] = HubFailure(HubError.Server, "坏了", status = 500)
        val slots = PluginSlots(hub, MutableStateFlow(true), MutableSharedFlow())
        backgroundScope.launch { slots.run(MutableStateFlow(true)) }
        runCurrent()
        assertFalse(slots.state.value.inboxItemsKnown)
        hub.failing.clear()
        slots.loadInboxItems()
        assertTrue(slots.state.value.inboxItemsKnown)
        assertEquals(listOf(event), slots.state.value.inboxItems)
    }

    @Test fun aHubWithoutSlotsIsNeverAskedForItemPages() = runTest {
        val hub = FakeHub(mutableMapOf("inbox_item" to listOf(event)))
        val slots = PluginSlots(hub, MutableStateFlow(false), MutableSharedFlow())
        backgroundScope.launch { slots.run(MutableStateFlow(true)) }
        runCurrent()
        slots.load()
        slots.loadInboxItems()
        assertTrue(hub.asked.isEmpty())
        assertTrue(slots.state.value.inboxItems.isEmpty())
    }

    /** A plugin drawing an item's page: what each call was told, and what it answers. */
    private class FakePlugin : PluginApiStub() {
        val renders = mutableListOf<Pair<PluginUiContext?, String?>>()
        val actions = mutableListOf<Triple<String, PluginUiContext?, Map<String, String>?>>()
        var view: () -> PluginView = { eventView }
        var result: () -> PluginUiResult = { PluginUiResult() }
        override suspend fun renderUi(plugin: String, ui: String, context: PluginUiContext?, state: String?): PluginView {
            assertEquals("lark-connector" to "event", plugin to ui)
            renders += context to state
            return view()
        }
        override suspend fun uiAction(plugin: String, ui: String, action: String, context: PluginUiContext?, state: String?,
            values: Map<String, String>?): PluginUiResult {
            actions += Triple(action, context, values)
            return result()
        }
    }

    private fun itemModel(plugin: PluginUi) =
        PluginViewModel(plugin, route.pluginId, route.uiId, PluginUiContext(item = route.itemId, size = "full"), opens = InboxItemPageText::opens)

    @Test fun drawsAndPressesWithTheItem() = runTest {
        val plugin = FakePlugin()
        val model = itemModel(plugin)
        assertNull("not before it has tried", InboxItemPageText.fallback(model.state.value))
        model.load()
        assertEquals(PluginUiContext(item = "lark:F1:1051", size = "full"), plugin.renders.single().first)
        assertNull(InboxItemPageText.fallback(model.state.value))

        // A button's toast (接着《…》 on an event that is gone); the view stays.
        plugin.result = { PluginUiResult(toast = "上周的评审已经不在信箱里了") }
        val pressed = model.press(PluginViewAction("continues"))
        assertEquals("上周的评审已经不在信箱里了", pressed.toast)
        assertEquals(Triple("continues", PluginUiContext(item = "lark:F1:1051", size = "full"), null), plugin.actions.last())
        // A reply submits the page's fields, and the plugin's new view replaces the page.
        model.edit("reply", "好的，周四见")
        plugin.result = { PluginUiResult(view = eventView.copy(state = "replied")) }
        model.press(PluginViewAction("reply", submit = true))
        assertEquals(mapOf("reply" to "好的，周四见"), plugin.actions.last().third)
        assertEquals("replied", model.state.value.view?.state)
        // Drawn again with the plugin's state.
        model.load()
        assertEquals("replied", plugin.renders.last().second)
    }

    // 在会话里看: the one app link the hub lets this slot open comes through to the page; https as anywhere.
    @Test fun aButtonOpensTheItemsSource() = runTest {
        val plugin = FakePlugin()
        val model = itemModel(plugin)
        model.load()
        plugin.result = { PluginUiResult(openUrl = "persona://inbox/lark:F1:1051/source") }
        assertEquals("persona://inbox/lark:F1:1051/source", model.press(PluginViewAction("open_chat")).openUrl)
        plugin.result = { PluginUiResult(openUrl = "https://example.com/doc") }
        assertEquals("https://example.com/doc", model.press(PluginViewAction("doc")).openUrl)
        plugin.result = { PluginUiResult(openUrl = "persona://open?target=settings") }
        assertNull(model.press(PluginViewAction("other")).openUrl)
        // Any other plugin page opens https only.
        val page = PluginViewModel(plugin, "lark-connector", "event")
        page.load()
        plugin.result = { PluginUiResult(openUrl = "persona://inbox/lark:F1:1051/source") }
        assertNull(page.press(PluginViewAction("open_chat")).openUrl)
    }

    @Test fun readsTheSourceLink() {
        assertEquals("lark:F1:1051", PluginViewText.inboxSource("persona://inbox/lark:F1:1051/source"))
        assertEquals("lark:F1:1051", PluginViewText.inboxSource("persona://inbox/lark%3AF1%3A1051/source"))
        assertEquals("a+b/c", PluginViewText.inboxSource("persona://inbox/a+b%2Fc/source"))
        assertEquals("mail-1", PluginViewText.inboxSource("PERSONA://inbox/mail-1/source"))
        listOf(null, "", "https://example.com/inbox/x/source", "jarvis://inbox/x/source", "persona://inbox/x", "persona://inbox//source",
            "persona://inbox/x/source/more", "persona://inbox/x/reply", "persona://open/x/source", "persona://inbox/x/source?y=1",
            "persona://inbox/x/source#top", "persona:inbox/x/source", "persona://inbox/a b/source").forEach {
            assertNull(it, PluginViewText.inboxSource(it))
        }
        assertTrue(InboxItemPageText.opens("persona://inbox/x/source"))
        assertTrue(InboxItemPageText.opens("https://example.com"))
        assertFalse(InboxItemPageText.opens("http://example.com"))
    }

    // A plugin that declines the item gives way quietly: the hub no longer has it (404 not_found), the device may only
    // read and answer (403), it says the item is not its own, or it draws nothing.
    @Test fun theItemsOwnPageOpensQuietlyWhenThePluginDeclines() = runTest {
        listOf<() -> PluginView>(
            { throw PluginUiNotFound("no such plugin") },
            { throw PluginUiForbidden("this device can only read and answer") },
            { throw PluginUiUnsupported() },
            { PluginView(sections = listOf(PluginViewSection(items = listOf(PluginViewItem("a_newer_kind"))))) },
        ).forEach { answer ->
            val model = itemModel(FakePlugin().apply { view = answer })
            model.load()
            assertEquals(InboxItemFallback.Quiet, InboxItemPageText.fallback(model.state.value))
        }
    }

    // It broke (502 plugin_failed) or the hub is out of reach: the own page, saying so with 重试.
    @Test fun theItemsOwnPageSaysWhenThePluginBroke() = runTest {
        val broken = itemModel(FakePlugin().apply { view = { throw HubFailure(HubError.Server, "插件出错了", status = 502, code = "plugin_failed") } })
        broken.load()
        assertEquals(InboxItemFallback.Notice("插件的页面没画出来：插件出错了"), InboxItemPageText.fallback(broken.state.value))
        val away = itemModel(FakePlugin().apply { view = { throw HubFailure(HubError.Unreachable) } })
        away.load()
        assertEquals(InboxItemFallback.Notice("插件的页面没画出来：连不上 hub"), InboxItemPageText.fallback(away.state.value))
    }

    @Test fun onceDrawnAFailureKeepsThePage() = runTest {
        val plugin = FakePlugin()
        val model = itemModel(plugin)
        model.load()
        plugin.view = { throw HubFailure(HubError.Server, "插件出错了", status = 502) }
        model.load()
        assertEquals("读取失败：插件出错了", model.state.value.error)
        assertNotNull(model.state.value.view)
        assertNull(InboxItemPageText.fallback(model.state.value))
        plugin.result = { throw HubFailure(HubError.Server, "插件出错了", status = 502) }
        assertEquals("没有做成：插件出错了", model.press(PluginViewAction("reply", submit = true)).error)
        assertNull(InboxItemPageText.fallback(model.state.value))
        // A plugin that then says the item is not its own gives way.
        plugin.result = { throw PluginUiUnsupported() }
        model.press(PluginViewAction("continues"))
        assertEquals(InboxItemFallback.Quiet, InboxItemPageText.fallback(model.state.value))
    }

    companion object {
        /**
         * The Feishu plugin's body of an event, as the contract describes it (people, time span, the event it continues,
         * the messages, reply, 在会话里看); the title, tier, summary and 已处理 are the app's header.
         */
        val eventView = PluginView(title = "周四的评审", state = "s1", sections = listOf(
            PluginViewSection(items = listOf(
                PluginViewItem("row", title = "涉及", value = "甲、丙、丁"),
                PluginViewItem("row", title = "时间", value = "10:01 – 10:06"),
                PluginViewItem("row", title = "接着《上周的评审》", action = PluginViewAction("continues")),
            )),
            PluginViewSection(title = "消息", items = listOf(
                PluginViewItem("row", title = "甲", subtitle = "周四下午评审 A 可以吗？", value = "10:01"),
                PluginViewItem("row", title = "丙", subtitle = "A 那个周四下午可以", value = "10:05"),
                PluginViewItem("row", title = "丁", subtitle = "B 的接口改了", value = "10:06"),
            )),
            PluginViewSection(items = listOf(
                PluginViewItem("field", name = "reply", label = "回复", kind = "text", placeholder = "写点什么"),
                PluginViewItem("button", title = "回复", action = PluginViewAction("reply", submit = true)),
                PluginViewItem("button", title = "在会话里看", action = PluginViewAction("open_chat")),
            )),
        ))
    }
}
