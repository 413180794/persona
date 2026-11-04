package persona.feature.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import persona.core.contracts.api.ConnectionState
import persona.core.contracts.GroupKeys
import persona.core.contracts.PluginUiEvent
import persona.core.contracts.api.PluginManagement
import persona.core.contracts.api.PluginUi
import persona.core.contracts.api.PluginUiContext
import persona.core.contracts.api.PluginUiPlace
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

// Plugins' UI slots (docs/plugins.md「UI 插槽」) on Android: the「插件」section at the top of the settings home (the
// cards first, then a row per nav page with its icon and badge), the settings tab's badge (the nav pages' badges added
// up), the sections plugins draw on a machine's page, and the pages they draw for their connectors' inbox items
// (PluginInboxItem.kt). A hub without plugin_ui_slots has none of them.

/** The slots this app draws; a placement it does not know is ignored. */
internal object PluginSlotText {
    const val NavPage = "nav_page"
    const val Card = "overview_card"
    const val MachineSection = "machine_section"
    const val InboxItem = "inbox_item"
    private val known = setOf(NavPage, Card, MachineSection)

    fun known(place: PluginUiPlace): Boolean = place.placement in known && place.plugin.isNotEmpty() && place.id.isNotEmpty()

    /** An inbox item's page: for the items of one connector; one that names none draws for nobody's. */
    fun inboxItem(place: PluginUiPlace): Boolean =
        place.placement == InboxItem && place.plugin.isNotEmpty() && place.id.isNotEmpty() && !place.connector.isNullOrBlank()

    /** A nav page's icon from the few a plugin may name; anything else is the plugin's puzzle piece. */
    fun icon(name: String?): PhIcon = when (name) {
        "chart" -> PhIcons.ChartBar
        "gauge" -> PhIcons.Gauge
        "list" -> PhIcons.ListChecks
        "bell" -> PhIcons.BellSimple
        "doc" -> PhIcons.FileText
        "calendar" -> PhIcons.CalendarBlank
        "money" -> PhIcons.Coins
        else -> PhIcons.PuzzlePiece
    }

    /**
     * The settings tab's badge from the nav pages': the numbers added up; when there are only words (「新」), a dot.
     */
    fun tabBadge(places: List<PluginUiPlace>): SlotBadge {
        val texts = places.filter { it.placement == NavPage }.mapNotNull { it.badge?.text?.trim()?.takeIf(String::isNotEmpty) }
        val count = texts.sumOf { it.toIntOrNull()?.coerceAtLeast(0) ?: 0 }
        return SlotBadge(count, dot = count == 0 && texts.any { it.toIntOrNull() == null })
    }

    /** A page's title for the row that opens it: the card's `opens` names a page of the same plugin. */
    fun opened(places: List<PluginUiPlace>, card: PluginUiPlace): PluginUiPlace? {
        val id = card.opens?.takeIf { it.isNotEmpty() } ?: return null
        return places.firstOrNull { it.plugin == card.plugin && it.id == id }
            ?: PluginUiPlace(plugin = card.plugin, id = id, title = card.title, placement = NavPage)
    }
}

/** A badge for the settings tab: a count, or a dot when the plugins' badges are words. */
data class SlotBadge(val count: Int = 0, val dot: Boolean = false)

data class PluginSlotsState(
    /** The hub has UI slots (`plugin_ui_slots`) and the list has been read at least once. */
    val available: Boolean = false,
    val places: List<PluginUiPlace> = emptyList(),
    val error: String? = null,
    /** The pages plugins draw for their connectors' inbox items (`inbox_item`); none on a hub that lists none. */
    val inboxItems: List<PluginUiPlace> = emptyList(),
    /** [inboxItems] has been read from this hub; until then an item opened (at launch) may turn out to be a plugin's. */
    val inboxItemsKnown: Boolean = false,
) {
    val navPages: List<PluginUiPlace> get() = places.filter { it.placement == PluginSlotText.NavPage }
    val cards: List<PluginUiPlace> get() = places.filter { it.placement == PluginSlotText.Card }
    val machineSections: List<PluginUiPlace> get() = places.filter { it.placement == PluginSlotText.MachineSection }
    val tabBadge: SlotBadge get() = PluginSlotText.tabBadge(places)
}

/**
 * Where the selected hub's plugins draw in this app, kept for the whole shell: read when the hub (with
 * `plugin_ui_slots`) is online and when the settings list shows, and the nav pages' badges changed by the hub's
 * `plugin_ui` events without reading the list again.
 */
class PluginSlots internal constructor(
    internal val api: PluginUi,
    private val slotsAvailable: Flow<Boolean>,
    private val events: Flow<PluginUiEvent>,
) {
    constructor(plugins: PluginManagement) : this(plugins, plugins.slotsAvailable, plugins.uiEvents)

    private val mutable = MutableStateFlow(PluginSlotsState())
    val state: StateFlow<PluginSlotsState> = mutable.asStateFlow()
    private var supported = false

    /** Runs while the shell does: reads the slots whenever the hub says it has them and is online, applies badges. */
    suspend fun run(online: Flow<Boolean>) = coroutineScope {
        launch { events.collect(::badge) }
        combine(slotsAvailable, online) { slots, up -> slots to up }.distinctUntilChanged().collectLatest { (slots, up) ->
            supported = slots
            if (!slots) mutable.value = PluginSlotsState()
            else if (up) load()
        }
    }

    /**
     * Reads the list again (the settings list shows, the hub reconnected), then the inbox items' pages, which the hub
     * lists by their placement only; a failure keeps what was there.
     */
    suspend fun load() {
        if (!supported) return
        try {
            val places = api.uiPlaces("").filter(PluginSlotText::known)
            mutable.update { it.copy(available = true, places = places, error = null) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (error: Exception) { mutable.update { it.copy(error = "插件的入口读不到：${failureReason(error)}") } }
        loadInboxItems()
    }

    /**
     * The `inbox_item` pages (#1683), read with the other slots and again whenever the inbox shows. A hub that does not
     * know the slot lists none; one that cannot answer keeps the pages known so far, and an item whose page cannot be
     * drawn opens its own page instead (rememberInboxItemBody).
     */
    suspend fun loadInboxItems() {
        if (!supported) return
        try {
            val pages = api.uiPlaces(PluginSlotText.InboxItem).filter(PluginSlotText::inboxItem)
            mutable.update { it.copy(inboxItems = pages, inboxItemsKnown = true) }
        } catch (cancelled: CancellationException) { throw cancelled }
        catch (_: Exception) { /* Nothing to tell: these items open their own pages. */ }
    }

    /** A `plugin_ui` event: the nav page's badge changes (none clears it). */
    fun badge(event: PluginUiEvent) = mutable.update { s ->
        s.copy(places = s.places.map { if (it.plugin == event.plugin && it.id == event.id && it.placement == PluginSlotText.NavPage) it.copy(badge = event.badge?.takeIf { b -> b.text.isNotBlank() }) else it })
    }
}

/** The shell's slots for the selected hub's plugins, running while the shell is composed; null in 体验模式. */
@Composable
fun rememberPluginSlots(plugins: PluginManagement?, connection: StateFlow<ConnectionState>): PluginSlots? {
    val slots = remember(plugins) { plugins?.let(::PluginSlots) } ?: return null
    LaunchedEffect(slots, connection) { slots.run(connection.map { it == ConnectionState.Online }) }
    return slots
}

/** Runs [model]'s `refresh_after` while the page is resumed (not in the background, not left for another page). */
@Composable
internal fun KeepFresh(model: PluginViewModel) {
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(model, lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.RESUMED) { model.keepFresh() } }
}

/**
 * The「插件」section at the top of the settings home: each card a plugin draws (`overview_card`, compact; tapping it
 * opens the page it names), then a row per nav page with its icon and badge. Nothing when no plugin draws there.
 */
internal fun LazyListScope.pluginSlotItems(
    state: PluginSlotsState,
    cards: List<PluginCardSlot>,
    selected: (PluginUiPlace) -> Boolean,
    onOpen: (PluginUiPlace) -> Unit,
) {
    val pages = state.navPages
    val shown = cards.filter { !it.state.unsupported }
    if (pages.isEmpty() && shown.isEmpty()) return
    item("slots-title") { JSectionHeader("插件") }
    shown.forEach { card ->
        item("slot-card-${card.place.plugin}-${card.place.id}") {
            val opens = PluginSlotText.opened(state.places, card.place)
            PluginCard(card.place, card.state, card.handle, onOpen = opens?.let { { onOpen(it) } }, onRetry = card.reload)
        }
    }
    pages.forEachIndexed { index, page ->
        item("slot-page-${page.plugin}-${page.id}") {
            JGroupItem(GroupPosition.of(index, pages.size), if (index == 0 && shown.isNotEmpty()) Modifier.padding(top = Tokens.Space.xs) else Modifier) {
                val badge = page.badge?.takeIf { it.text.isNotBlank() }
                JRow(page.title.ifBlank { page.id }, leadingIcon = PluginSlotText.icon(page.icon), selected = selected(page),
                    subtitle = page.pluginName?.takeIf { it.isNotBlank() && it != page.title },
                    trailing = badge?.let { b -> { JTag(b.text, PluginViewText.tone(b.tone)) } },
                    chevron = true, onClick = { onOpen(page) })
            }
        }
    }
}

/** A card a plugin draws: its title, the compact view, and a chevron when tapping it opens a page. */
@Composable
private fun PluginCard(place: PluginUiPlace, state: PluginViewState, handle: PluginViewHandle, onOpen: (() -> Unit)?, onRetry: () -> Unit) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    JCard(Modifier.padding(bottom = Tokens.Space.m), onClick = onOpen) {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(state.view?.title?.takeIf { it.isNotBlank() } ?: place.title.ifBlank { place.id }, style = type.headline, color = palette.ink,
                        maxLines = 1, overflow = TextOverflow.Ellipsis)
                    place.pluginName?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink3, maxLines = 1, overflow = TextOverflow.Ellipsis) }
                }
                if (state.loading && state.view != null) { Spacer(Modifier.width(Tokens.Space.s)); JSpinner() }
                if (onOpen != null) { Spacer(Modifier.width(Tokens.Space.s)); JIcon(PhIcons.CaretRight, null, size = Tokens.Space.l, tint = palette.ink3) }
            }
            when {
                state.view != null -> PluginCardContent(state, handle)
                state.error != null -> Row(verticalAlignment = Alignment.CenterVertically) {
                    JInlineStatus(state.error, Modifier.weight(1f), tone = InlineTone.Failed)
                    Spacer(Modifier.width(Tokens.Space.s))
                    JButton("重试", onRetry, kind = JButtonKind.Secondary, size = JButtonSize.Small, enabled = !state.loading)
                }
                else -> Text("正在读取…", style = type.caption, color = palette.ink3, maxLines = 1)
            }
        }
    }
}

/** A card of the settings home as drawn now: where it is, its view, its buttons, and reading it again (重试). */
internal class PluginCardSlot(val place: PluginUiPlace, val state: PluginViewState, val handle: PluginViewHandle, val reload: () -> Unit)

/** The cards of the settings home, each drawn compact and kept fresh while settings is on screen. */
@Composable
internal fun rememberPluginCards(slots: PluginSlots?): List<PluginCardSlot> {
    slots ?: return emptyList()
    val state by slots.state.collectAsState()
    val cards = state.cards
    val models = remember(slots, cards.map { it.plugin to it.id }) {
        cards.map { it to PluginViewModel(slots.api, it, PluginUiContext(size = "compact")) }
    }
    val scope = rememberCoroutineScope()
    return models.map { (place, model) ->
        key(place.plugin, place.id) {
            LaunchedEffect(model) { model.load() }
            KeepFresh(model)
            PluginCardSlot(place, model.state.collectAsState().value, rememberPluginView(model)) { scope.launch { model.load() } }
        }
    }
}

/**
 * The sections plugins draw on machine [machineId]'s page (`machine_section`, after the machine's own), as a lazy
 * list's items: each with its title, kept fresh while the page is resumed. A plugin whose hub does not know the
 * machine (one of another hub) draws nothing; a failure shows with 重试.
 */
@Composable
fun rememberMachinePluginSections(slots: PluginSlots?, machineId: String): LazyListScope.() -> Unit {
    slots ?: return {}
    val state by slots.state.collectAsState()
    val places = state.machineSections
    // The hub knows its machines by their own ids (no group suffix).
    val context = remember(machineId) { PluginUiContext(machine = GroupKeys.raw(machineId), size = "full") }
    val models = remember(slots, context, places.map { it.plugin to it.id }) {
        places.map { it to PluginViewModel(slots.api, it, context) }
    }
    val scope = rememberCoroutineScope()
    val sections = models.map { (place, model) ->
        key(place.plugin, place.id) {
            LaunchedEffect(model) { model.load() }
            KeepFresh(model)
            Triple(place to model, model.state.collectAsState().value, rememberPluginView(model))
        }
    }
    return {
        sections.forEach { (pair, view, handle) ->
            val (place, model) = pair
            if (view.unsupported || (view.view == null && view.error == null)) return@forEach
            val key = "plugin-${place.plugin}-${place.id}"
            item("$key-title") { JSectionHeader(view.view?.title?.takeIf { it.isNotBlank() } ?: place.title.ifBlank { place.id }) }
            if (view.view == null && view.error != null) item("$key-error") {
                JNotice(view.error, Modifier.padding(top = Tokens.Space.xs), status = Status.NeedsYou, actionLabel = "重试",
                    onAction = { scope.launch { model.load() } })
            } else if (view.error != null) item("$key-error") {
                JInlineStatus(view.error, Modifier.padding(vertical = Tokens.Space.xs), tone = InlineTone.Failed)
            }
            pluginViewItems(key, view, handle, showTitles = true)
        }
    }
}
