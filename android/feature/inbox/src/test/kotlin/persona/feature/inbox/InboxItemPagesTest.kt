package persona.feature.inbox

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.ExtensionUiPlace
import persona.core.domain.inbox.InboxItem
import persona.core.navigation.InboxItemUiRoute
import org.junit.Assert.*
import org.junit.Test

// docs/extensions.md「UI 插槽」inbox_item (#1683): an item whose connector's extension draws its page opens that page; every
// other item, and every item on a hub that lists no such page, opens its own page as before.
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class InboxItemPagesTest {
    /** The app's strings; Robolectric runs in 简体中文 (robolectric.properties). */
    private val res get() = androidx.test.core.app.ApplicationProvider.getApplicationContext<android.content.Context>().resources
    private val lark = InboxRecord(InboxItem("lark:F1:1051", kind = "group", title = "周四的评审"), feedId = "F1", startPosition = 1051, source = "lark")
    private val mail = InboxRecord(InboxItem("mail-1", kind = "mail", title = "订单"), source = "mail")
    private val moment = InboxRecord(InboxItem("moment-1", kind = "moment"), source = "lark", moment = InboxMomentRef(postId = "post-1"))
    private val event = ExtensionUiPlace("lark-connector", "event", "事件", "inbox_item", connector = "lark")

    @Test fun aConnectorsExtensionDrawsItsItems() {
        val pages = InboxItemPages(listOf(event))
        assertEquals(InboxItemUiRoute("lark:F1:1051", "lark-connector", "event", "周四的评审"), pages.route(lark))
        assertEquals(InboxItemUiRoute("lark:F1:1051", "lark-connector", "event", "周四的评审"), inboxOpenPage(lark, pages))
        // Every item of the connector: a 公司圈 post from it too.
        assertEquals("moment-1", pages.route(moment)?.itemId)
        // Another connector's items keep their own page.
        assertNull(pages.route(mail))
        assertEquals(MailRoute("mail-1"), inboxOpenPage(mail, pages))
    }

    @Test fun withoutAPageEveryItemOpensAsBefore() {
        // An older hub lists nothing at inbox_item.
        val none = InboxItemPages()
        assertEquals(LarkRoute("F1", "lark:F1:1051", 1051), inboxOpenPage(lark, none))
        assertEquals(MailRoute("mail-1"), inboxOpenPage(mail, none))
        assertEquals(LarkMomentRoute("post-1", "moment-1"), inboxOpenPage(moment, none))
        // Pages that cannot be for this item: another slot, another connector, no connector, no extension or id.
        val others = InboxItemPages(listOf(event.copy(placement = "nav_page"), event.copy(connector = "github"), event.copy(connector = null),
            event.copy(extension = ""), event.copy(id = "")))
        assertNull(others.route(lark))
        // An item that says nothing of its source is nobody's.
        assertNull(InboxItemPages(listOf(event.copy(connector = ""))).route(lark.copy(source = "")))
    }

    @Test fun theFirstListedPageWins() {
        val pages = InboxItemPages(listOf(event.copy(connector = "github"), event, event.copy(extension = "another", id = "thread")))
        assertEquals("lark-connector" to "event", pages.place(lark)?.let { it.extension to it.id })
    }

    @Test fun theOwnPageIsWhatTheListOpenedBefore() {
        assertEquals(LarkRoute("F1", "lark:F1:1051", 1051), inboxItemPage(lark))
        assertEquals(MailRoute("mail-1"), inboxItemPage(mail))
        assertEquals(LarkMomentRoute("post-1", "moment-1"), inboxItemPage(moment))
    }

    // persona://inbox/<id>/source from an extension's page: that item's own page, as the list opened it before.
    @Test fun theSourceLinkOpensTheItemsOwnPage() {
        val records = listOf(lark, mail, moment)
        assertEquals(LarkRoute("F1", "lark:F1:1051", 1051), inboxSourcePage("lark:F1:1051", records))
        assertEquals(MailRoute("mail-1"), inboxSourcePage("mail-1", records))
        assertEquals(LarkMomentRoute("post-1", "moment-1"), inboxSourcePage("moment-1", records))
        assertNull(inboxSourcePage("gone", records))
    }

    // The header carries the tier (the extension's body does not), once triaged.
    @Test fun theHeaderSaysTheTier() {
        assertEquals(persona.core.domain.inbox.InboxTier.Action, inboxHeaderTier(lark.copy(item = lark.item.copy(tier = "action"))))
        assertEquals(persona.core.domain.inbox.InboxTier.Fyi, inboxHeaderTier(lark.copy(item = lark.item.copy(tier = "fyi"))))
        assertNull(inboxHeaderTier(lark))
    }

    // The header's source line says what the item's row says.
    @Test fun theSourceLineIsTheRows() {
        assertEquals(listOf("飞书", "群聊", "3 条消息"), inboxSourceTags(res, lark.copy(sourceLabel = "飞书", count = 3)))
        assertEquals(listOf("工作账号", "群聊"), inboxSourceTags(res, lark.copy(sourceLabel = "工作账号")))
        // A mail is a mail once.
        assertEquals(listOf("邮件"), inboxSourceTags(res, mail))
    }

    // At launch, before the hub's list is read, an item opens its own page and the shell hears of it; once the list
    // comes, those pages whose connector's extension draws them get the extension's page in their place.
    @Test fun anItemOpenedBeforeTheListIsKnownTurnsIntoTheExtensionsPage() {
        val heard = mutableListOf<androidx.navigation3.runtime.NavKey>()
        val unknown = InboxItemPages(known = false, provisional = { heard += it })
        assertEquals(LarkRoute("F1", "lark:F1:1051", 1051), unknown.open(lark))
        assertEquals(MailRoute("mail-1"), unknown.open(mail))
        assertEquals(listOf(LarkRoute("F1", "lark:F1:1051", 1051), MailRoute("mail-1")), heard)
        // Known: nothing is provisional, whatever opens.
        val known = InboxItemPages(listOf(event), provisional = { heard += it })
        heard.clear()
        assertEquals(InboxItemUiRoute("lark:F1:1051", "lark-connector", "event", "周四的评审"), known.open(lark))
        known.open(mail)
        assertTrue(heard.isEmpty())

        val chatLink = LarkRoute("F1", "lark:F1:1051", 1051).copy(startPosition = 1060)
        val stack = listOf(persona.core.navigation.InboxRoute, LarkRoute("F1", "lark:F1:1051", 1051), MailRoute("mail-1"), chatLink)
        val provisional = setOf(LarkRoute("F1", "lark:F1:1051", 1051), MailRoute("mail-1"))
        // The Feishu item becomes the extension's page; the mail has no extension; a chat link was never provisional.
        assertEquals(listOf(LarkRoute("F1", "lark:F1:1051", 1051) to InboxItemUiRoute("lark:F1:1051", "lark-connector", "event", "周四的评审")),
            known.swaps(stack, provisional, listOf(lark, mail, moment)))
        // An older hub lists no pages: nothing changes.
        assertTrue(InboxItemPages().swaps(stack, provisional, listOf(lark, mail)).isEmpty())
        // Pages that have left the stack are not brought back.
        assertTrue(known.swaps(listOf(persona.core.navigation.InboxRoute), provisional, listOf(lark)).isEmpty())
    }

    @Test fun theListMarksTheItemItsExtensionPageShows() {
        val shown = InboxItemUiRoute("lark:F1:1051", "lark-connector", "event")
        assertTrue(isInboxItemSelected(shown, "lark:F1:1051", "F1"))
        assertFalse(isInboxItemSelected(shown, "lark:F1:1200", "F1"))
        assertFalse(isInboxItemSelected(shown, "mail-1", null))
    }

    // #2323: a Feishu document's notice (lark-connector's `doc` block, no chat behind it) opened the mail page, which failed
    // with 「邮件加载失败」. Only a mail opens the mail page; a document, and an item of a shape the app does not know,
    // open the item's detail page.
    @Test fun aDocumentsNoticeOpensItsOwnPageAndOnlyMailOpensTheMailPage() {
        val wire = kotlinx.serialization.json.Json.parseToJsonElement("""{"url":"https://example.larkoffice.com/wiki/W1",
            "link":"https://example.larkoffice.com/wiki/W1","type":"sheet","notice":"mention","notice_id":"n1","read_at":0,
            "reply":{"kind":"cell","label":"评论 @你的那个单元格"},"future":{"x":1},
            "rows":[{"ref":"r1","table":"任务","title":"压测报告","fields":[{"name":"跟进人","value":"@示例同事"}],
              "status":[{"field":"f1","name":"状态","value":"进行中","options":[{"name":"进行中"},{"name":"已完成"}]}]}]}""")
        val ref = requireNotNull(inboxDocRef(wire as kotlinx.serialization.json.JsonObject))
        assertEquals("https://example.larkoffice.com/wiki/W1", ref.url)
        assertEquals("sheet", ref.type)
        assertEquals("评论 @你的那个单元格", ref.reply)
        assertEquals(listOf(InboxDocRow("压测报告", "任务", listOf(InboxDocField("跟进人", "@示例同事")), listOf(InboxDocField("状态", "进行中")))), ref.rows)
        assertTrue(isLarkDocumentUrl(ref.url))
        // Nothing to open: no block.
        assertNull(inboxDocRef(kotlinx.serialization.json.Json.parseToJsonElement("""{"type":"docx"}""") as kotlinx.serialization.json.JsonObject))

        val doc = InboxRecord(InboxItem("doc:T1:1730700000000", kind = "doc", title = "Q4 计划"), source = "lark", doc = ref)
        val none = InboxItemPages()
        assertEquals(InboxItemDetailRoute("doc:T1:1730700000000"), inboxItemPage(doc))
        assertEquals(InboxItemDetailRoute("doc:T1:1730700000000"), inboxOpenPage(doc, none))
        assertEquals(InboxItemDetailRoute("doc:T1:1730700000000"), inboxSourcePage("doc:T1:1730700000000", listOf(doc)))
        // Chats, posts and mail open as before; a mail known only by its kind (a record cached without its source) too.
        assertEquals(LarkRoute("F1", "lark:F1:1051", 1051), inboxItemPage(lark))
        assertEquals(LarkMomentRoute("post-1", "moment-1"), inboxItemPage(moment))
        assertEquals(MailRoute("mail-1"), inboxItemPage(mail))
        assertEquals(MailRoute("mail-2"), inboxItemPage(InboxRecord(InboxItem("mail-2", kind = "mail"))))
        // A shape the app does not know (a connector's thread, nothing to say what it is) is not a mail.
        assertEquals(InboxItemDetailRoute("github:1"), inboxItemPage(InboxRecord(InboxItem("github:1", kind = "unknown"), source = "github")))
        assertEquals(InboxItemDetailRoute("x"), inboxItemPage(InboxRecord(InboxItem("x"))))
        // The list marks it, its row says what kind of notice, and a cached copy still opens as a document.
        assertTrue(isInboxItemSelected(InboxItemDetailRoute("doc:T1:1730700000000"), "doc:T1:1730700000000", null))
        assertEquals("@我", kindLabel(res, doc))
        assertEquals("评论", kindLabel(res, doc.copy(doc = ref.copy(notice = "comment"))))
        val cached = doc.cached().record()
        assertEquals(ref, cached.doc)
        assertEquals(InboxItemDetailRoute("doc:T1:1730700000000"), inboxItemPage(cached))
        // An extension's page that was opened before the list was known gives way for a document too.
        val pages = InboxItemPages(listOf(event))
        val key = InboxItemDetailRoute("doc:T1:1730700000000")
        assertEquals(listOf(key to InboxItemUiRoute("doc:T1:1730700000000", "lark-connector", "event", "Q4 计划")),
            pages.swaps(listOf(key), setOf(key), listOf(doc)))
    }
}
