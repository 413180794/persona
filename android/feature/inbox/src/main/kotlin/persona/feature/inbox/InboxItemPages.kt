package persona.feature.inbox

import androidx.compose.foundation.layout.padding
import androidx.compose.runtime.Composable
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.ui.Modifier
import androidx.navigation3.runtime.NavKey
import persona.core.contracts.api.PluginUiPlace
import persona.core.designsystem.JNotice
import persona.core.designsystem.Status
import persona.core.designsystem.Tokens
import persona.core.navigation.InboxItemUiRoute

// An inbox item's page drawn by a plugin (docs/plugins.md「UI 插槽」`inbox_item`, #1683): a plugin's connector (the
// Feishu plugin's `lark`) may draw the body of the items it brings in. Opening such an item shows the app's header of
// the item over the plugin's view (InboxItemPluginScreen); every other item, and every item on a hub that lists no
// such page, opens its own page as before. When the plugin cannot draw an item, its own page opens instead, saying so
// in a line with 重试 when the plugin broke rather than declined.

/**
 * The `inbox_item` pages the hub lists, by the connector whose items each draws; [failures] are the items whose page
 * broke (item id → what to say), shown on their own pages until [retry] tries the plugin's page again in place of
 * the page it is called from. Until the hub's list is [known] (at launch, before its hello), an item opens its own
 * page and [provisional] hears of it, so that the shell can put the plugin's page in its place once the list comes.
 */
data class InboxItemPages(
    val places: List<PluginUiPlace> = emptyList(),
    val failures: Map<String, String> = emptyMap(),
    val retry: (from: NavKey, itemId: String) -> Unit = { _, _ -> },
    val known: Boolean = true,
    val provisional: (NavKey) -> Unit = {},
) {
    /** The plugin's page for [record], when the connector it comes from has one; the first listed wins. */
    fun place(record: InboxRecord): PluginUiPlace? {
        val source = record.source.takeIf { it.isNotEmpty() } ?: return null
        return places.firstOrNull { it.placement == Placement && it.connector == source && it.plugin.isNotEmpty() && it.id.isNotEmpty() }
    }

    /** Where opening [record] goes when a plugin draws its page; null: the item's own page. */
    fun route(record: InboxRecord): InboxItemUiRoute? = place(record)?.let { InboxItemUiRoute(record.item.id, it.plugin, it.id, record.item.title) }

    /** Opening [record] (from the list, or an item link): [inboxOpenPage], told to [provisional] while the list is unknown. */
    fun open(record: InboxRecord): NavKey = inboxOpenPage(record, this).also(::opened)

    /** [page] was opened for an item: an own page opened before the list was known may be the plugin's to draw. */
    fun opened(page: NavKey) { if (!known && page !is InboxItemUiRoute) provisional(page) }

    /**
     * Once the list is known: the item pages of [stack] opened before it was ([provisional]) whose connector's plugin
     * draws them, each with the plugin's page to put in its place. A chat link was never provisional, so it stays a chat.
     */
    fun swaps(stack: List<NavKey>, provisional: Set<NavKey>, records: List<InboxRecord>): List<Pair<NavKey, InboxItemUiRoute>> =
        stack.filter { it in provisional }.mapNotNull { key ->
            val itemId = when (key) { is LarkRoute -> key.itemId; is MailRoute -> key.itemId; is LarkMomentRoute -> key.itemId; else -> null }
            records.firstOrNull { it.item.id == itemId }?.let(::route)?.let { key to it }
        }

    companion object {
        const val Placement = "inbox_item"
    }
}

/** The shell's list of `inbox_item` pages; none in 体验模式 and on a hub without them. */
val LocalInboxItemPages = compositionLocalOf { InboxItemPages() }

/** The item's own page: a 公司圈 post, its Feishu chat at the item's first message, or the mail. */
fun inboxItemPage(record: InboxRecord): NavKey = record.moment?.let { LarkMomentRoute(it.postId, record.item.id) }
    ?: record.feedId?.let { LarkRoute(it, record.item.id, record.startPosition) }
    ?: MailRoute(record.item.id)

/** What opening [record] shows: its plugin's page when [pages] has one for its connector, else its own page. */
fun inboxOpenPage(record: InboxRecord, pages: InboxItemPages): NavKey = pages.route(record) ?: inboxItemPage(record)

/**
 * Where a plugin's `persona://inbox/<id>/source` goes: item [itemId]'s own page, what opening it did before plugins
 * drew items (for Feishu, the chat at the item's first message). Null when the item is not in [records].
 */
fun inboxSourcePage(itemId: String, records: List<InboxRecord>): NavKey? = records.firstOrNull { it.item.id == itemId }?.let(::inboxItemPage)

/**
 * On an item's own page opened because its plugin's page broke: the one line saying so, with 重试 (the plugin's page
 * again, in place of [from]). Nothing otherwise.
 */
@Composable
internal fun InboxPluginNotice(itemId: String?, from: NavKey, modifier: Modifier = Modifier.padding(bottom = Tokens.Space.s)) {
    val pages = LocalInboxItemPages.current
    val message = itemId?.let { pages.failures[it] } ?: return
    JNotice(message, modifier, status = Status.NeedsYou, actionLabel = "重试", onAction = { pages.retry(from, itemId) })
}
