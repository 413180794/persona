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
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4

// An inbox item's page drawn by its connector's extension (docs/extensions/extensions.md "UI slots" inbox_item, #1683): which pages the
// hub lists, drawing and pressing with the item in the context, and when the item's own page opens instead.
/** Robolectric for the words: the app's strings in Simplified Chinese (robolectric.properties). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ExtensionInboxItemTest {
    private val res get() = ApplicationProvider.getApplicationContext<android.content.Context>().resources

    private val event = ExtensionUiPlace("larkext", "event", "事件", "inbox_item", extensionName = "飞书", connector = "lark")
    private val route = InboxItemUiRoute("lark:F1:1051", "larkext", "event", "周四的评审")

    private class FakeHub(val byPlacement: MutableMap<String, List<ExtensionUiPlace>>) : ExtensionApiStub() {
        val asked = mutableListOf<String>()
        val failing = mutableMapOf<String, Exception>()
        override suspend fun uiPlaces(placement: String, launcher: String?, machine: String?): List<ExtensionUiPlace> {
            asked += placement
            failing[placement]?.let { throw it }
            return byPlacement[placement].orEmpty()
        }
    }

    @Test fun readsTheInboxItemPagesByTheirPlacement() = runTest {
        val hub = FakeHub(mutableMapOf(
            "" to listOf(ExtensionUiPlace("larkext", "accounts", "账号", "nav_page")),
            // One without a connector draws for nobody's items; another slot listed here is not an item's page.
            "inbox_item" to listOf(event, event.copy(id = "loose", connector = null), event.copy(id = "card", placement = "overview_card")),
        ))
        val slotsOn = MutableStateFlow(true)
        val slots = ExtensionSlots(hub, slotsOn, MutableSharedFlow())
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
        assertNull(slots.state.value.error?.text(res))

        // An older hub does not know the slot and lists nothing there; nor does a failing list of the other slots
        // stop the inbox items' pages from being read.
        hub.failing.clear()
        hub.failing[""] = HubFailure(HubError.Server, "坏了", status = 500)
        hub.byPlacement["inbox_item"] = emptyList()
        slots.load()
        assertTrue(slots.state.value.inboxItems.isEmpty())
        assertEquals("扩展的入口读不到：坏了", slots.state.value.error?.text(res))

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
        val slots = ExtensionSlots(hub, MutableStateFlow(true), MutableSharedFlow())
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
        val slots = ExtensionSlots(hub, MutableStateFlow(false), MutableSharedFlow())
        backgroundScope.launch { slots.run(MutableStateFlow(true)) }
        runCurrent()
        slots.load()
        slots.loadInboxItems()
        assertTrue(hub.asked.isEmpty())
        assertTrue(slots.state.value.inboxItems.isEmpty())
    }

    /** An extension drawing an item's page: what each call was told, and what it answers. */
    private class FakeExtension : ExtensionApiStub() {
        val renders = mutableListOf<Pair<ExtensionUiContext?, String?>>()
        val actions = mutableListOf<Triple<String, ExtensionUiContext?, Map<String, String>?>>()
        var view: () -> ExtensionView = { eventView }
        var result: () -> ExtensionUiResult = { ExtensionUiResult() }
        override suspend fun renderUi(extension: String, ui: String, context: ExtensionUiContext?, state: String?): ExtensionView {
            assertEquals("larkext" to "event", extension to ui)
            renders += context to state
            return view()
        }
        override suspend fun uiAction(extension: String, ui: String, action: String, context: ExtensionUiContext?, state: String?,
            values: Map<String, String>?): ExtensionUiResult {
            actions += Triple(action, context, values)
            return result()
        }
    }

    private fun itemModel(extension: ExtensionUi) =
        ExtensionViewModel(extension, route.extensionId, route.uiId, ExtensionUiContext(item = route.itemId, size = "full"), opens = InboxItemPageText::opens)

    @Test fun drawsAndPressesWithTheItem() = runTest {
        val extension = FakeExtension()
        val model = itemModel(extension)
        assertNull("not before it has tried", InboxItemPageText.fallback(res, model.state.value))
        model.load()
        assertEquals(ExtensionUiContext(item = "lark:F1:1051", size = "full"), extension.renders.single().first)
        assertNull(InboxItemPageText.fallback(res, model.state.value))

        // A button's toast ("Continuing <...>" on an event that is gone); the view stays.
        extension.result = { ExtensionUiResult(toast = "上周的评审已经不在信箱里了") }
        val pressed = model.press(ExtensionViewAction("continues"))
        assertEquals("上周的评审已经不在信箱里了", pressed.toast)
        assertEquals(Triple("continues", ExtensionUiContext(item = "lark:F1:1051", size = "full"), null), extension.actions.last())
        // A reply submits the page's fields, and the extension's new view replaces the page.
        model.edit("reply", "好的，周四见")
        extension.result = { ExtensionUiResult(view = eventView.copy(state = "replied")) }
        model.press(ExtensionViewAction("reply", submit = true))
        assertEquals(mapOf("reply" to "好的，周四见"), extension.actions.last().third)
        assertEquals("replied", model.state.value.view?.state)
        // Drawn again with the extension's state.
        model.load()
        assertEquals("replied", extension.renders.last().second)
    }

    // "View in conversation": the one app link the hub lets this slot open comes through to the page; https as anywhere.
    @Test fun aButtonOpensTheItemsSource() = runTest {
        val extension = FakeExtension()
        val model = itemModel(extension)
        model.load()
        extension.result = { ExtensionUiResult(openUrl = "persona://inbox/lark:F1:1051/source") }
        assertEquals("persona://inbox/lark:F1:1051/source", model.press(ExtensionViewAction("open_chat")).openUrl)
        extension.result = { ExtensionUiResult(openUrl = "https://example.com/doc") }
        assertEquals("https://example.com/doc", model.press(ExtensionViewAction("doc")).openUrl)
        extension.result = { ExtensionUiResult(openUrl = "persona://open?target=settings") }
        assertNull(model.press(ExtensionViewAction("other")).openUrl)
        // Any other extension page opens https only.
        val page = ExtensionViewModel(extension, "larkext", "event")
        page.load()
        extension.result = { ExtensionUiResult(openUrl = "persona://inbox/lark:F1:1051/source") }
        assertNull(page.press(ExtensionViewAction("open_chat")).openUrl)
    }

    @Test fun readsTheSourceLink() {
        assertEquals("lark:F1:1051", ExtensionViewText.inboxSource("persona://inbox/lark:F1:1051/source"))
        assertEquals("lark:F1:1051", ExtensionViewText.inboxSource("persona://inbox/lark%3AF1%3A1051/source"))
        assertEquals("a+b/c", ExtensionViewText.inboxSource("persona://inbox/a+b%2Fc/source"))
        assertEquals("mail-1", ExtensionViewText.inboxSource("PERSONA://inbox/mail-1/source"))
        listOf(null, "", "https://example.com/inbox/x/source", "example://inbox/x/source", "persona://inbox/x", "persona://inbox//source",
            "persona://inbox/x/source/more", "persona://inbox/x/reply", "persona://open/x/source", "persona://inbox/x/source?y=1",
            "persona://inbox/x/source#top", "persona:inbox/x/source", "persona://inbox/a b/source").forEach {
            assertNull(it, ExtensionViewText.inboxSource(it))
        }
        assertTrue(InboxItemPageText.opens("persona://inbox/x/source"))
        assertTrue(InboxItemPageText.opens("https://example.com"))
        assertFalse(InboxItemPageText.opens("http://example.com"))
    }

    // An extension that declines the item gives way quietly: the hub no longer has it (404 not_found), the device may only
    // read and answer (403), it says the item is not its own, or it draws nothing.
    @Test fun theItemsOwnPageOpensQuietlyWhenTheExtensionDeclines() = runTest {
        listOf<() -> ExtensionView>(
            { throw ExtensionUiNotFound("no such plugin") },
            { throw ExtensionUiForbidden("this device can only read and answer") },
            { throw ExtensionUiUnsupported() },
            { ExtensionView(sections = listOf(ExtensionViewSection(items = listOf(ExtensionViewItem("a_newer_kind"))))) },
        ).forEach { answer ->
            val model = itemModel(FakeExtension().apply { view = answer })
            model.load()
            assertEquals(InboxItemFallback.Quiet, InboxItemPageText.fallback(res, model.state.value))
        }
    }

    // It broke (502 extension_failed) or the hub is out of reach: the own page, saying so with "Retry".
    @Test fun theItemsOwnPageSaysWhenTheExtensionBroke() = runTest {
        val broken = itemModel(FakeExtension().apply { view = { throw HubFailure(HubError.Server, "扩展出错了", status = 502, code = "plugin_failed") } })
        broken.load()
        assertEquals(InboxItemFallback.Notice("扩展的页面没画出来：扩展出错了"), InboxItemPageText.fallback(res, broken.state.value))
        val away = itemModel(FakeExtension().apply { view = { throw HubFailure(HubError.Unreachable) } })
        away.load()
        assertEquals(InboxItemFallback.Notice("扩展的页面没画出来：连不上 hub"), InboxItemPageText.fallback(res, away.state.value))
    }

    @Test fun onceDrawnAFailureKeepsThePage() = runTest {
        val extension = FakeExtension()
        val model = itemModel(extension)
        model.load()
        extension.view = { throw HubFailure(HubError.Server, "扩展出错了", status = 502) }
        model.load()
        assertEquals("读取失败：扩展出错了", model.state.value.error?.text(res))
        assertNotNull(model.state.value.view)
        assertNull(InboxItemPageText.fallback(res, model.state.value))
        extension.result = { throw HubFailure(HubError.Server, "扩展出错了", status = 502) }
        assertEquals("没有做成：扩展出错了", model.press(ExtensionViewAction("reply", submit = true)).error?.text(res))
        assertNull(InboxItemPageText.fallback(res, model.state.value))
        // An extension that then says the item is not its own gives way.
        extension.result = { throw ExtensionUiUnsupported() }
        model.press(ExtensionViewAction("continues"))
        assertEquals(InboxItemFallback.Quiet, InboxItemPageText.fallback(res, model.state.value))
    }

    companion object {
        /**
         * The Feishu extension's body of an event, as the contract describes it (people, time span, the event it continues,
         * the messages, reply, "View in conversation"); the title, tier, summary and "Done" are the app's header.
         */
        val eventView = ExtensionView(title = "周四的评审", state = "s1", sections = listOf(
            ExtensionViewSection(items = listOf(
                ExtensionViewItem("row", title = "涉及", value = "甲、丙、丁"),
                ExtensionViewItem("row", title = "时间", value = "10:01 – 10:06"),
                ExtensionViewItem("row", title = "接着《上周的评审》", action = ExtensionViewAction("continues")),
            )),
            ExtensionViewSection(title = "消息", items = listOf(
                ExtensionViewItem("row", title = "甲", subtitle = "周四下午评审 A 可以吗？", value = "10:01"),
                ExtensionViewItem("row", title = "丙", subtitle = "A 那个周四下午可以", value = "10:05"),
                ExtensionViewItem("row", title = "丁", subtitle = "B 的接口改了", value = "10:06"),
            )),
            ExtensionViewSection(items = listOf(
                ExtensionViewItem("field", name = "reply", label = "回复", kind = "text", placeholder = "写点什么"),
                ExtensionViewItem("button", title = "回复", action = ExtensionViewAction("reply", submit = true)),
                ExtensionViewItem("button", title = "在会话里看", action = ExtensionViewAction("open_chat")),
            )),
        ))
    }
}
