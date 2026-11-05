package persona.feature.settings

import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.res.stringResource
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyItemScope
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.tween
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import persona.core.contracts.api.ExtensionViewAction
import persona.core.contracts.api.ExtensionViewItem
import persona.core.contracts.api.ExtensionViewPush
import persona.core.contracts.api.ExtensionViewSection
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import kotlinx.coroutines.launch

// Draws an extension's View (docs/extensions.md「声明式 UI」「UI 插槽」) with this app's design system: each kind of item as
// a component of the style in use (charts and number grids from the design system's), tones as role colours. The
// extension gives no colour, size or icon. A kind this app does not know is skipped.

/**
 * What the page does for a view: press a button (after its confirm), choose for a choice field, open a link, switch a
 * segmented control (its action goes at once), and since UI version 3 search, filter and load more.
 */
internal class ExtensionViewHandle(
    val press: (ExtensionViewItem, ExtensionViewAction) -> Unit,
    val edit: (String, String) -> Unit,
    val choose: (ExtensionViewItem) -> Unit,
    val open: (String) -> Unit,
    val segment: (ExtensionViewItem, String) -> Unit = { _, _ -> },
    val search: (String) -> Unit = {},
    val filter: (String) -> Unit = {},
    val more: (Int, ExtensionViewSection) -> Unit = { _, _ -> },
    /** UI version 4: a code hosting component's action, with its own values (a path, a line, the text typed). */
    val act: (ExtensionViewAction, Map<String, String>) -> Unit = { _, _ -> },
    /** UI version 4: a tab was chosen. */
    val tab: (String) -> Unit = {},
    /** UI version 4: a file tree without an action shows that file's diff on the page. */
    val reveal: (String) -> Unit = {},
    /** UI version 5: a page of diffs' viewed marks, whitespace, lines and quiet actions; none where nothing acts. */
    val review: CodeReviewState? = null,
)

/**
 * The view's dialogs (a button's confirm, a choice field's choices) and the way its buttons go: an action's toast
 * shows as the app's notice, its link opens in the app, a new view replaces the page. [appLink] takes the app's own
 * links a page may open (an inbox item's source) before https ones go to the app's link handling. A push (UI version
 * 3) goes to [onPush] (a page of the extension drawn on top), a pop to [onPop] (back).
 */
@Composable
internal fun rememberExtensionView(model: ExtensionViewModel, appLink: (String) -> Boolean = { false },
                                   onPush: (ExtensionViewPush) -> Unit = {}, onPop: () -> Unit = {}): ExtensionViewHandle {
    val scope = rememberCoroutineScope()
    val feedback = LocalAppFeedback.current
    val uri = LocalUriHandler.current
    val res = LocalResources.current
    val state by model.state.collectAsState()
    var confirming by remember(model) { mutableStateOf<Pair<ExtensionViewItem, ExtensionViewAction>?>(null) }
    var confirmingValues by remember(model) { mutableStateOf<Map<String, String>?>(null) }
    var choosing by remember(model) { mutableStateOf<ExtensionViewItem?>(null) }
    val routes by rememberUpdatedState(appLink)
    val push by rememberUpdatedState(onPush)
    val pop by rememberUpdatedState(onPop)
    val open: (String) -> Unit = { url -> if (!routes(url) && ExtensionViewText.isHttps(url)) runCatching { uri.openUri(url) } }
    fun show(outcome: ExtensionActionOutcome) {
        outcome.toast?.let { feedback?.notice(it) }
        outcome.error?.let { feedback?.notice(it.text(res)) }
        outcome.openUrl?.let(open)
        outcome.push?.let(push)
        if (outcome.pop) pop()
    }
    fun run(action: ExtensionViewAction, values: Map<String, String>? = null) = scope.launch { show(model.press(action, values)) }
    val prefs = LocalContext.current.getSharedPreferences(CodeReviewState.Prefs, android.content.Context.MODE_PRIVATE)
    val review = remember(model) {
        CodeReviewState(prefs) { action, values, done ->
            scope.launch {
                val result = model.fetch(action, values)
                if (result == null) feedback?.notice(res.getString(R.string.settings_ext_review_quiet_failed))
                else result.toast?.takeIf { it.isNotBlank() }?.let { feedback?.notice(it) }
                done(result)
            }
        }
    }
    val handle = remember(model) {
        ExtensionViewHandle(
            press = { item, action -> if (!action.confirm.isNullOrBlank()) confirming = item to action else run(action) },
            edit = model::edit,
            choose = { choosing = it },
            open = open,
            segment = { item, value -> scope.launch { show(model.choose(item, value)) } },
            search = { query -> scope.launch { show(model.search(query)) } },
            filter = { value -> scope.launch { show(model.filter(value)) } },
            more = { index, section -> scope.launch { show(model.more(index, section)) } },
            act = { action, values ->
                if (!action.confirm.isNullOrBlank()) { confirmingValues = values; confirming = ExtensionViewItem(type = "button") to action } else run(action, values)
            },
            tab = { value -> scope.launch { show(model.tab(value)) } },
            review = review,
        )
    }
    confirming?.let { (item, action) ->
        JDialog({ confirming = null }, title = ExtensionViewText.confirmTitle(res, item), text = action.confirm,
            confirm = ExtensionViewText.confirmLabel(res, item) to { confirming = null; run(action, confirmingValues); confirmingValues = null },
            destructive = item.role == "destructive")
    }
    choosing?.let { item ->
        val name = item.name.orEmpty()
        val current = state.values[name]
        JActionSheet(item.choices.orEmpty().map { choice ->
            JAction(choice.label, checked = current == choice.value, onClick = { model.edit(name, choice.value) })
        }, { choosing = null }, title = item.label ?: name)
    }
    return handle
}

/**
 * The view's sections and items, into a page's list: [key] keeps two views on one page apart. Rows, links, switches
 * and choices that follow each other form one group; the rest stand on their own.
 */
internal fun LazyListScope.extensionViewItems(key: String, state: ExtensionViewState, handle: ExtensionViewHandle, showTitles: Boolean = true) {
    val view = state.view ?: return
    view.tabs?.takeIf { it.items.size >= 2 && it.name.isNotEmpty() }?.let { tabs ->
        item("$key-tabs") { ExtensionTabs(tabs, state.values[tabs.name] ?: tabs.value, state, handle, Modifier.padding(bottom = Tokens.Space.s)) }
    }
    // a page of diffs (UI version 5): viewed so far, open threads, whitespace, the pending review
    if (reviewing(view) && handle.review != null) item("$key-review-tools") { ReviewToolbar(view, state, handle, Modifier.padding(bottom = Tokens.Space.s)) }
    if (view.search != null || view.filter != null) item("$key-search") {
        ExtensionSearchBar(view, state, handle, Modifier.padding(bottom = Tokens.Space.s))
    }
    // a split view (UI version 4): a phone has no room beside the code, so the side pane (the file tree) folds into a
    // bar on top that opens in place, and closes once a file is picked
    val split = view.layout == "split"
    val side = if (split) view.sections.filter { it.pane == "side" } else emptyList()
    if (side.isNotEmpty()) item("$key-side") { SidePane(side, state, handle) }
    view.sections.forEachIndexed { s, section ->
        if (section in side) return@forEachIndexed
        val title = section.title?.takeIf { it.isNotBlank() }
        if (title != null && showTitles) item("$key-s$s-title") { JSectionHeader(title) }
        else if (s > 0) item("$key-s$s-gap") { Spacer(Modifier.padding(top = Tokens.Space.l)) }
        val items = section.items.filter(ExtensionViewText::drawable)
        // The extension's keys keep a list's place when it is drawn again (a page's 加载更多, a search).
        val keys = ExtensionViewText.itemKeys(items)
        var i = 0
        while (i < items.size) {
            val start = i
            if (ExtensionViewText.grouped(items[i])) while (i < items.size && ExtensionViewText.grouped(items[i])) i++ else i++
            val run = items.subList(start, i)
            run.forEachIndexed { index, item ->
                item(keys[start + index]?.let { "$key-s$s-k-$it" } ?: "$key-s$s-i${start + index}") {
                    // drawn again, a block whose key is new fades in and one that went fades out (#2811)
                    val moving = Modifier.itemMotion(this, PersonaTheme.reduceMotion, PersonaTheme.motion.standard)
                    if (ExtensionViewText.grouped(item)) {
                        JGroupItem(GroupPosition.of(index, run.size), moving.then(if (index == 0 && start > 0) Modifier.padding(top = Tokens.Space.s) else Modifier)) {
                            GroupedItem(item, state, handle)
                        }
                    } else Box(moving) { StandaloneItem(item, state, handle, first = start == 0) }
                }
            }
        }
        if (section.more?.action?.id?.isNotEmpty() == true) item("$key-s$s-more") {
            JGroupItem(GroupPosition.Single, Modifier.padding(top = Tokens.Space.s)) { ExtensionMore(s, section, state, handle) }
        }
        section.footer?.takeIf { it.isNotBlank() }?.let { footer ->
            item("$key-s$s-footer") { SettingsCaption(footer, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.s)) }
        }
    }
    if (reviewing(view) && view.review != null && handle.review != null) item("$key-review-bar") { ReviewBar(view, state, handle, Modifier.padding(top = Tokens.Space.l)) }
}

/**
 * How a block of a page's list comes and goes when the view is drawn again: fades in where its key is new, out where
 * its key went, the rest slide to their new places; nothing with reduced motion (the list's first draw never moves).
 */
private fun Modifier.itemMotion(scope: LazyItemScope, reduce: Boolean, duration: Int): Modifier =
    if (reduce) this else with(scope) {
        animateItem(fadeInSpec = tween(duration), placementSpec = tween(duration, easing = FastOutSlowInEasing), fadeOutSpec = tween(duration))
    }

/** A row, a link, a switch or a choice: one line of a group. */
@Composable
internal fun GroupedItem(item: ExtensionViewItem, state: ExtensionViewState, handle: ExtensionViewHandle) {
    val enabled = !state.busy
    when (item.type) {
        "row" -> {
            val action = item.action?.takeIf { it.id.isNotEmpty() }
            val badge = item.badge?.takeIf { it.text.isNotBlank() }
            JRow(item.title.orEmpty(), subtitle = item.subtitle?.takeIf { it.isNotBlank() }, value = item.value?.takeIf { it.isNotBlank() },
                leadingIcon = item.icon?.takeIf { it.isNotBlank() }?.let(ExtensionIcons::of), leading = rowLeading(item),
                trailing = when {
                    action != null && state.acting == action.id -> ({ JSpinner() })
                    badge != null -> ({ JTag(badge.text, ExtensionViewText.tone(badge.tone), animated = true) })
                    else -> null
                },
                chevron = action != null, enabled = enabled || action == null, changingValue = true,
                onClick = action?.let { a -> { handle.press(item, a) } })
        }
        "link" -> JRow(item.title.orEmpty(), leadingIcon = PhIcons.ArrowSquareOut, chevron = false, onClick = { handle.open(item.url.orEmpty()) })
        "field" -> {
            val name = item.name.orEmpty()
            val value = state.values[name].orEmpty()
            if (item.kind == "toggle") {
                JSwitchRow(item.label.orEmpty(), value == "true", { on -> handle.edit(name, if (on) "true" else "false") }, enabled = enabled)
            } else {
                val label = item.choices.orEmpty().firstOrNull { it.value == value }?.label ?: value.ifEmpty { item.placeholder ?: stringResource(R.string.settings_ext_not_chosen) }

                JRow(item.label.orEmpty(), value = label, enabled = enabled, onClick = { handle.choose(item) })
            }
        }
    }
}

/** Text, a number, a progress bar, a button, a text field, a divider: each on its own. */
@Composable
internal fun StandaloneItem(item: ExtensionViewItem, state: ExtensionViewState, handle: ExtensionViewHandle, first: Boolean) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val top = if (first) Modifier else Modifier.padding(top = Tokens.Space.s)
    when (item.type) {
        "text" -> {
            val text = item.text.orEmpty()
            when (item.style) {
                "code" -> JCodeBlock(text, top, small = true)
                "caption" -> Text(text, top.padding(horizontal = rowPadding), style = type.caption, color = palette.ink3)
                "secondary" -> Text(text, top.padding(horizontal = rowPadding), style = type.callout, color = palette.ink2)
                else -> Text(text, top.padding(horizontal = rowPadding), style = type.body, color = palette.ink)
            }
        }
        "metric" -> JGroupItem(GroupPosition.Single, top) {
            Column(Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.xxs)) {
                Text(item.label.orEmpty(), style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val tone = ExtensionViewText.tone(item.tone)
                JChangingText(item.value.orEmpty(), style = type.number, color = if (tone == Tone.Neutral) palette.ink else palette.tone(tone).text)
                item.detail?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink3) }
            }
        }
        "progress" -> JGroupItem(GroupPosition.Single, top) {
            Column(Modifier.fillMaxWidth().padding(horizontal = rowPadding, vertical = Tokens.Space.m), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(item.label.orEmpty(), Modifier.weight(1f), style = type.body, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Spacer(Modifier.width(Tokens.Space.s))
                    Text(ExtensionViewText.percent(item), style = type.micro, color = palette.ink2, maxLines = 1)
                }
                JLinearProgress(ExtensionViewText.ratio(item), tone = ExtensionViewText.tone(item.tone))
                item.detail?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink3) }
            }
        }
        "button" -> {
            val action = item.action ?: return
            JButton(item.title.orEmpty(), { handle.press(item, action) }, top.fillMaxWidth(),
                kind = when {
                    item.role == "destructive" -> JButtonKind.Destructive
                    action.submit -> JButtonKind.Primary
                    else -> JButtonKind.Secondary
                },
                loading = state.acting == action.id, enabled = !state.busy)
        }
        "field" -> FieldBox(item, state, handle, top)
        "divider" -> JDivider(top.padding(vertical = Tokens.Space.s))
        "chart" -> JGroupItem(GroupPosition.Single, top) { ChartItem(item, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m), compact = false) }
        "stat_grid" -> JGroupItem(GroupPosition.Single, top) {
            JStatGrid(ExtensionViewText.stats(item), Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m))
        }
        "segmented" -> SegmentedItem(item, state, handle, top)
        "markdown" -> ExtensionMarkdown(item.text.orEmpty(), top.padding(horizontal = rowPadding))
        "table" -> JGroupItem(GroupPosition.Single, top) { ExtensionTable(item, Modifier.padding(horizontal = rowPadding, vertical = Tokens.Space.m)) }
        "empty" -> JEmptyState(item.title.orEmpty(), item.detail.orEmpty(), top, icon = item.icon?.takeIf { it.isNotBlank() }?.let(ExtensionIcons::of),
            action = item.button?.takeIf { it.title.isNotBlank() && it.action.id.isNotEmpty() }?.let { b ->
                { JButton(b.title, { handle.press(item, b.action) }, kind = JButtonKind.Secondary, loading = state.acting == b.action.id,
                    enabled = !state.busy) }
            })
        "loading" -> JLoading(top, item.title?.takeIf { it.isNotBlank() })
        "html" -> LocalViewHtml.current?.invoke(item, top)
        "header" -> CodeHeader(item, top)
        "breadcrumb" -> CodeBreadcrumb(item, state, handle, top)
        "file_tree" -> CodeFileTree(item, state, handle, top)
        "code" -> CodeFile(item, state, handle, top)
        "diff" -> CodeDiff(item, state, handle, top)
        "comment_thread" -> CodeCommentThread(item, state, handle, top)
        "composer" -> CodeComposer(item, state, handle, top)
    }
}

/**
 * How an `html` item is drawn: only an assistant's card provides it (AssistantViewCard); elsewhere the item is not
 * drawn at all (the hub never lets an extension's view have one).
 */
internal val LocalViewHtml = staticCompositionLocalOf<(@Composable (ExtensionViewItem, Modifier) -> Unit)?> { null }

/**
 * A view drawn in place, not in a lazy list (an assistant's ```persona-view in a reply): the same items in the same
 * groups as a page's, one section under another.
 */
@Composable
internal fun ExtensionViewColumn(state: ExtensionViewState, handle: ExtensionViewHandle, modifier: Modifier = Modifier) {
    val view = state.view ?: return
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        view.title?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.headline, color = palette.ink) }
        view.sections.forEach { section ->
            section.title?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink2) }
            val items = section.items.filter(ExtensionViewText::drawable)
            var i = 0
            while (i < items.size) {
                val start = i
                if (ExtensionViewText.grouped(items[i])) while (i < items.size && ExtensionViewText.grouped(items[i])) i++ else i++
                val run = items.subList(start, i)
                if (ExtensionViewText.grouped(run.first())) JGroup(rows = run.map { item -> { GroupedItem(item, state, handle) } })
                else run.forEach { StandaloneItem(it, state, handle, first = true) }
            }
            section.footer?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink3) }
        }
    }
}

/** A chart from the design system; a card's is short, with no scale. */
@Composable
private fun ChartItem(item: ExtensionViewItem, modifier: Modifier, compact: Boolean) {
    val data = remember(item) { ExtensionViewText.chart(item) } ?: return
    JChart(data, modifier, compact = compact, title = item.title?.takeIf { it.isNotBlank() }, detail = item.detail?.takeIf { it.isNotBlank() })
}

/**
 * One of a few, switched at once (a time range): the style's segmented control in the width there is (the board's
 * cells share it out, so it must not sit in a sideways scroll: its cells collapsed there).
 */
@Composable
private fun SegmentedItem(item: ExtensionViewItem, state: ExtensionViewState, handle: ExtensionViewHandle, modifier: Modifier) {
    val choices = item.choices.orEmpty()
    val current = state.values[item.name.orEmpty()] ?: item.value
    Box(modifier.fillMaxWidth()) {
        JSegmented(choices.map { JSegment(it.label) }, choices.indexOfFirst { it.value == current },
            { index -> if (!state.busy) choices.getOrNull(index)?.let { handle.segment(item, it.value) } })
    }
}

/**
 * An extension's compact view (a card, `size: compact`), not lazily: its sections one under another, no fields. Rows and
 * links are plain rows, buttons small.
 */
@Composable
internal fun ExtensionCardContent(state: ExtensionViewState, handle: ExtensionViewHandle, modifier: Modifier = Modifier) {
    val view = state.view ?: return
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
        view.sections.forEach { section ->
            val items = section.items.filter(ExtensionViewText::drawableOnCard)
            if (items.isEmpty() && section.footer.isNullOrBlank()) return@forEach
            section.title?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis) }
            items.forEach { item -> CardItem(item, state, handle) }
            section.footer?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink3) }
        }
    }
}

@Composable
private fun CardItem(item: ExtensionViewItem, state: ExtensionViewState, handle: ExtensionViewHandle) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    when (item.type) {
        "text" -> Text(item.text.orEmpty(), style = when (item.style) { "caption" -> type.caption; "secondary" -> type.callout; "code" -> type.code; else -> type.body },
            color = when (item.style) { "caption" -> palette.ink3; "secondary" -> palette.ink2; else -> palette.ink })
        "metric" -> JStatGrid(listOf(JStat(item.label.orEmpty(), item.value.orEmpty(), item.detail, ExtensionViewText.tone(item.tone))))
        "progress" -> Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(item.label.orEmpty(), Modifier.weight(1f), style = type.callout, color = palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(ExtensionViewText.percent(item), style = type.micro, color = palette.ink2, maxLines = 1)
            }
            JLinearProgress(ExtensionViewText.ratio(item), tone = ExtensionViewText.tone(item.tone))
        }
        "row", "link" -> CardLine(item, state, handle)
        "button" -> item.action?.let { action ->
            JButton(item.title.orEmpty(), { handle.press(item, action) }, kind = if (item.role == "destructive") JButtonKind.Destructive else JButtonKind.Secondary,
                size = JButtonSize.Small, loading = state.acting == action.id, enabled = !state.busy)
        }
        "divider" -> JDivider()
        "chart" -> ChartItem(item, Modifier, compact = true)
        "stat_grid" -> JStatGrid(ExtensionViewText.stats(item))
        "segmented" -> SegmentedItem(item, state, handle, Modifier)
        "markdown" -> ExtensionMarkdown(item.text.orEmpty())
        "empty" -> Text(item.title.orEmpty(), style = type.callout, color = palette.ink2)
        "loading" -> Row(verticalAlignment = Alignment.CenterVertically) {
            JSpinner()
            item.title?.takeIf { it.isNotBlank() }?.let { Spacer(Modifier.width(Tokens.Space.s)); Text(it, style = type.caption, color = palette.ink3) }
        }
    }
}

/** A text, secret or number field; a secret is never filled in by the extension and never shown. */
@Composable
private fun FieldBox(item: ExtensionViewItem, state: ExtensionViewState, handle: ExtensionViewHandle, modifier: Modifier) {
    val name = item.name.orEmpty()
    val value = state.values[name].orEmpty()
    val secret = item.kind == "secret"
    val number = item.kind == "number"
    JTextField(value, { text -> if (!number || ExtensionViewText.isNumber(text)) handle.edit(name, text) }, modifier.fillMaxWidth(),
        label = item.label, placeholder = item.placeholder?.takeIf { it.isNotBlank() }, singleLine = true, enabled = !state.busy,
        visualTransformation = if (secret) PasswordVisualTransformation() else androidx.compose.ui.text.input.VisualTransformation.None,
        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, keyboardType = when {
            secret -> KeyboardType.Password
            number -> KeyboardType.Decimal
            else -> KeyboardType.Text
        }))
}

/** A row or a link on a card: on the card's own text column (a settings row would indent it a second time). */
@Composable
private fun CardLine(item: ExtensionViewItem, state: ExtensionViewState, handle: ExtensionViewHandle) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val link = item.type == "link"
    val action = item.action?.takeIf { it.id.isNotEmpty() && !link }
    val click: (() -> Unit)? = when {
        link -> ({ handle.open(item.url.orEmpty()) })
        action != null -> ({ handle.press(item, action) })
        else -> null
    }
    Row(Modifier.fillMaxWidth().then(if (click != null) Modifier.clickable(enabled = !state.busy || link, onClick = click) else Modifier)
        .padding(vertical = Tokens.Space.xs), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(item.title.orEmpty(), style = type.body, color = if (link) palette.accent else palette.ink, maxLines = 1, overflow = TextOverflow.Ellipsis)
            item.subtitle?.takeIf { it.isNotBlank() && !link }?.let { Text(it, style = type.caption, color = palette.ink2, maxLines = 1, overflow = TextOverflow.Ellipsis) }
        }
        item.value?.takeIf { it.isNotBlank() && !link }?.let {
            Spacer(Modifier.width(Tokens.Space.s))
            JChangingText(it, style = type.callout, color = palette.ink3)
        }
        item.badge?.takeIf { it.text.isNotBlank() && !link }?.let {
            Spacer(Modifier.width(Tokens.Space.s))
            JTag(it.text, ExtensionViewText.tone(it.tone), animated = true)
        }
        if (action != null && state.acting == action.id) { Spacer(Modifier.width(Tokens.Space.s)); JSpinner() }
        else if (click != null) {
            Spacer(Modifier.width(Tokens.Space.xs))
            JIcon(if (link) PhIcons.ArrowSquareOut else PhIcons.CaretRight, null, size = Tokens.Space.l, tint = palette.ink3)
        }
    }
}
