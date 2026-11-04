package persona.feature.settings

import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.runtime.*
import persona.core.contracts.api.PluginUiContext
import persona.core.contracts.api.PluginUiForbidden
import persona.core.designsystem.*
import persona.core.navigation.InboxItemUiRoute
import kotlinx.coroutines.launch

// The body of an inbox item's page drawn by the plugin of the connector it comes from (docs/plugins.md「UI 插槽」
// `inbox_item`, #1683): the plugin's view of the item, drawn with the same renderer as its other pages, the item's id
// in the context. The inbox keeps its own header above it (feature:inbox's InboxItemPluginScreen); the shell puts the
// two together and opens the item's own page when the plugin cannot draw it.

/** Why an inbox item shows its own page instead of the plugin's body. */
sealed interface InboxItemFallback {
    /** The plugin does not draw this item here: a device that may only read (403), 404, `unsupported`, nothing drawn. */
    data object Quiet : InboxItemFallback

    /** Drawing it broke (502 `plugin_failed`, the hub out of reach): the item's own page says so in a line, with 重试. */
    data class Notice(val message: String) : InboxItemFallback
}

internal object InboxItemPageText {
    /**
     * Whether the item's own page takes over, and how: before the first view, a refusal is quiet and a breakdown is
     * told; a view with nothing this app draws is quiet. Once a view has been drawn, a failed refresh or button keeps
     * it and says so, as on the plugin's other pages; only a plugin that then says the item is not its own gives way.
     */
    fun fallback(state: PluginViewState): InboxItemFallback? {
        if (state.unsupported) return InboxItemFallback.Quiet
        val view = state.view
        if (view == null) {
            val failure = state.failure ?: return null
            if (state.loading) return null
            return if (failure is PluginUiForbidden) InboxItemFallback.Quiet
            else InboxItemFallback.Notice("插件的页面没画出来：${failureReason(failure)}")
        }
        return InboxItemFallback.Quiet.takeIf { view.sections.none { it.items.any(PluginViewText::drawable) } }
    }

    /** What a button may open on an item's page: https, and an inbox item's source. */
    fun opens(url: String): Boolean = PluginViewText.isHttps(url) || PluginViewText.inboxSource(url) != null
}

/**
 * The plugin's body of an inbox item's page as it is now: the view's [title] (none until it has one), whether it is
 * being read for the first time ([loading]) or again ([refreshing]), the [fallback] when the plugin cannot draw it,
 * and its [items] for the page's list.
 */
class InboxItemBody internal constructor(
    val title: String?,
    val loading: Boolean,
    val refreshing: Boolean,
    val fallback: InboxItemFallback?,
    val refresh: () -> Unit,
    val items: LazyListScope.() -> Unit,
)

/**
 * Item [route]'s body as its connector's plugin draws it, refreshed while on screen when the view asks
 * (`refresh_after`) and drawn again whenever the item changes ([revision]: new messages, done). A button that answers
 * `persona://inbox/<id>/source` calls [onSource] with that item's id; other links are https and open in the app. No
 * plugins here (体验模式): the item's own page, quietly.
 */
@Composable
fun rememberInboxItemBody(slots: PluginSlots?, route: InboxItemUiRoute, revision: Any? = null, onSource: (String) -> Unit): InboxItemBody {
    val api = slots?.api ?: return InboxItemBody(null, false, false, InboxItemFallback.Quiet, {}, {})
    val model = remember(api, route) {
        PluginViewModel(api, route.pluginId, route.uiId, PluginUiContext(item = route.itemId, size = "full"), opens = InboxItemPageText::opens)
    }
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val source by rememberUpdatedState(onSource)
    val handle = rememberPluginView(model) { url -> PluginViewText.inboxSource(url)?.let { source(it); true } ?: false }
    LaunchedEffect(model, revision) { model.load() }
    KeepFresh(model)
    val reload = remember(model) { { scope.launch { model.load() }; Unit } }
    return InboxItemBody(
        title = state.view?.title?.takeIf { it.isNotBlank() },
        loading = state.view == null && state.failure == null && !state.unsupported,
        refreshing = state.loading && state.view != null,
        fallback = InboxItemPageText.fallback(state),
        refresh = reload,
    ) {
        // A refresh or a button that failed after the view was drawn: one line above it.
        state.error?.takeIf { state.view != null }?.let { error ->
            item("item-body-error") { JInlineStatus(error, tone = InlineTone.Failed) }
        }
        pluginViewItems("item-body", state, handle)
    }
}
