package persona.feature.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.platform.UriHandler
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextOverflow
import coil3.compose.AsyncImage
import coil3.compose.rememberAsyncImagePainter
import com.mikepenz.markdown.m3.Markdown
import com.mikepenz.markdown.m3.markdownColor
import com.mikepenz.markdown.m3.markdownTypography
import kotlinx.coroutines.delay
import persona.core.contracts.api.ExtensionView
import persona.core.contracts.api.ExtensionViewItem
import persona.core.contracts.api.ExtensionViewSection
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons

// The UI version 3 parts of an extension's view (docs/extensions.md「声明式 UI」): markdown, a table, a row's icon /
// avatar / thumbnail, the search box and filter over a list, a section's 加载更多. Drawn with the design system only.

/** The fixed icons an extension may name, as this app's; one it does not know is the extension icon. */
internal object ExtensionIcons {
    fun of(name: String?): PhIcon = when (name) {
        "chart" -> PhIcons.ChartBar
        "gauge" -> PhIcons.Gauge
        "list" -> PhIcons.ListChecks
        "bell" -> PhIcons.BellSimple
        "doc" -> PhIcons.FileText
        "calendar" -> PhIcons.CalendarBlank
        "money" -> PhIcons.Coins
        "person" -> PhIcons.User
        "chat" -> PhIcons.ChatCircle
        "mail" -> PhIcons.EnvelopeSimple
        "link" -> PhIcons.LinkSimple
        "folder" -> PhIcons.FolderSimple
        "star" -> PhIcons.Star
        "check" -> PhIcons.CheckCircle
        "warning" -> PhIcons.Warning
        "clock" -> PhIcons.Clock
        "tag" -> PhIcons.Hash
        "search" -> PhIcons.MagnifyingGlass
        "settings" -> PhIcons.Gear
        else -> PhIcons.PuzzlePiece
    }
}

/** A row's leading picture, if it has one: its icon, avatar (a picture, or its letters) or thumbnail. */
internal fun rowLeading(item: ExtensionViewItem): (@Composable () -> Unit)? = when {
    item.thumbnail != null -> ({ RowThumbnail(item) })
    item.avatar != null -> ({ RowAvatar(item) })
    else -> null
}

@Composable
private fun RowAvatar(item: ExtensionViewItem) {
    val url = ExtensionViewText.picture(item.avatar?.url)
    val painter = url?.let { rememberAsyncImagePainter(it) }
    // The letters stand in until the picture has loaded, and when it never does.
    val loaded = painter?.state?.collectAsState()?.value is coil3.compose.AsyncImagePainter.State.Success
    JAvatar(ExtensionViewText.initials(item), size = Tokens.Space.xxxl, image = painter?.takeIf { loaded })
}

@Composable
private fun RowThumbnail(item: ExtensionViewItem) {
    val thumbnail = item.thumbnail ?: return
    val shape = RoundedCornerShape(Tokens.Radius.badge)
    val wide = thumbnail.aspect == "wide"
    // The well shows while it loads and when it never does.
    Box(Modifier.height(Tokens.MinTouch.android).aspectRatio(if (wide) 16f / 9f else 1f).clip(shape)
        .background(PersonaTheme.palette.sunken), contentAlignment = Alignment.Center) {
        ExtensionViewText.picture(thumbnail.url)?.let { url ->
            AsyncImage(url, item.title, Modifier.matchParentSize(), contentScale = ContentScale.Crop)
        }
    }
}

/** Markdown an extension wrote, in this app's type and colours; only https links open (in the app). */
@Composable
internal fun ExtensionMarkdown(text: String, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val outer = LocalUriHandler.current
    val links = remember(outer) {
        object : UriHandler {
            override fun openUri(uri: String) { if (ExtensionViewText.isHttps(uri)) outer.openUri(uri) }
        }
    }
    val source = remember(text) { ExtensionViewText.safeMarkdown(text) }
    CompositionLocalProvider(LocalUriHandler provides links) {
        Markdown(source, modifier = modifier.fillMaxWidth(),
            colors = markdownColor(text = palette.ink, codeBackground = palette.sunken, inlineCodeBackground = palette.sunken,
                dividerColor = palette.line, tableBackground = palette.surface, darkTheme = palette.dark),
            typography = markdownTypography(h1 = type.title, h2 = type.headline, h3 = type.bodyStrong, h4 = type.bodyStrong,
                h5 = type.callout, h6 = type.caption, text = type.body, paragraph = type.body, code = type.code,
                inlineCode = type.code, quote = type.callout.copy(color = palette.ink2), ordered = type.body, bullet = type.body,
                list = type.body, table = type.body, alertTitle = type.bodyStrong,
                textLink = TextLinkStyles(SpanStyle(color = palette.accent, textDecoration = TextDecoration.Underline))))
    }
}

/**
 * A table: a header, then the rows, each cell on one line. Wider than the page it scrolls sideways rather than squeezing
 * its columns into wrapping. A grid: a column is as wide as its widest cell, a row as tall as its tallest (a row of
 * Chinese is taller than one of digits), cells centred in their row, `end` columns flush right.
 */
@Composable
internal fun ExtensionTable(item: ExtensionViewItem, modifier: Modifier = Modifier) {
    val palette = PersonaTheme.palette
    val type = PersonaTheme.type
    val columns = item.columns.orEmpty().take(8)
    val rows = remember(item) { ExtensionViewText.tableRows(item) }
    val ends = columns.map { it.align == "end" }
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        TableGrid(columns.size, ends, Modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
            columns.forEach { Text(it.title, style = type.caption, color = palette.ink2, maxLines = 1, softWrap = false) }
            rows.forEach { row -> row.forEach { Text(it, style = type.callout, color = palette.ink, maxLines = 1, softWrap = false) } }
        }
        item.caption?.takeIf { it.isNotBlank() }?.let { Text(it, style = type.caption, color = palette.ink3) }
    }
}

/** Lays [content] (cells row after row, [count] to a row) out as a grid with the design system's gaps. */
@Composable
private fun TableGrid(count: Int, ends: List<Boolean>, modifier: Modifier, content: @Composable () -> Unit) {
    val density = LocalDensity.current
    val gapX = with(density) { Tokens.Space.l.roundToPx() }
    val gapY = with(density) { Tokens.Space.s.roundToPx() }
    Layout(content, modifier) { measurables, constraints ->
        val cells = measurables.map { it.measure(Constraints()) }
        if (count == 0 || cells.isEmpty()) return@Layout layout(0, 0) {}
        val rowCount = (cells.size + count - 1) / count
        val widths = IntArray(count) { c -> (0 until rowCount).maxOf { r -> cells.getOrNull(r * count + c)?.width ?: 0 } }
        val heights = IntArray(rowCount) { r -> (0 until count).maxOf { c -> cells.getOrNull(r * count + c)?.height ?: 0 } }
        // At least as wide as offered, so it starts at the leading edge instead of being centred.
        val width = maxOf(widths.sum() + gapX * (count - 1), constraints.minWidth)
        val height = heights.sum() + gapY * (rowCount - 1)
        layout(width, height) {
            var y = 0
            for (r in 0 until rowCount) {
                var x = 0
                for (c in 0 until count) {
                    cells.getOrNull(r * count + c)?.let { cell ->
                        val dx = if (ends.getOrElse(c) { false }) widths[c] - cell.width else 0
                        cell.place(x + dx, y + (heights[r] - cell.height) / 2)
                    }
                    x += widths[c] + gapX
                }
                y += heights[r] + gapY
            }
        }
    }
}

/**
 * The search box over a list and the filter beside it (UI version 3). Typing waits 0.3 s before asking the extension;
 * what is typed stays while the answer comes (the view's value is only where it starts).
 */
@Composable
internal fun ExtensionSearchBar(view: ExtensionView, state: ExtensionViewState, handle: ExtensionViewHandle, modifier: Modifier = Modifier) {
    val search = view.search
    val filter = view.filter?.takeIf { it.choices.size >= 2 && it.name.isNotEmpty() }
    var query by remember { mutableStateOf(state.query ?: search?.value.orEmpty()) }
    var typed by remember { mutableStateOf(false) }
    var choosing by remember { mutableStateOf(false) }
    if (search != null && typed) LaunchedEffect(query) {
        delay(ExtensionViewText.SearchPauseMillis)
        handle.search(query)
    }
    Row(modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        if (search != null) {
            JSearchField(query, { query = it; typed = true }, search.placeholder?.takeIf { it.isNotBlank() } ?: "搜索", Modifier.weight(1f))
        }
        if (filter != null) {
            val current = state.values[filter.name] ?: filter.value
            val label = filter.choices.firstOrNull { it.value == current }?.label ?: filter.title?.takeIf { it.isNotBlank() } ?: "筛选"
            JChip(label, selected = current != null && current != filter.choices.first().value, icon = PhIcons.FunnelSimple,
                onClick = { choosing = true })
        }
    }
    if (choosing && filter != null) {
        val current = state.values[filter.name] ?: filter.value
        JActionSheet(filter.choices.map { choice ->
            JAction(choice.label, checked = current == choice.value, onClick = { handle.filter(choice.value) })
        }, { choosing = false }, title = filter.title?.takeIf { it.isNotBlank() })
    }
}

/**
 * A section's 加载更多: coming on screen asks for more once (the list composes it as it nears the end), tapping asks
 * again after a failure; it spins while the answer is on its way.
 */
@Composable
internal fun ExtensionMore(index: Int, section: ExtensionViewSection, state: ExtensionViewState, handle: ExtensionViewHandle) {
    val more = section.more ?: return
    LaunchedEffect(section) { handle.more(index, section) }
    JRow(more.title?.takeIf { it.isNotBlank() } ?: "加载更多",
        trailing = if (state.loadingMore == index) ({ JSpinner() }) else null, chevron = false,
        enabled = state.loadingMore == null, onClick = { handle.more(index, section) })
}
