package jarvis.android.ui.files

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import java.io.IOException
import jarvis.android.data.HubException
import jarvis.android.ui.JTheme
import jarvis.android.ui.design.EmptyState
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.PageHeader
import jarvis.android.ui.design.SegmentedControl

// Shared pieces of the files screen's pages, on the design system (iOS FilesPane / GitPane / FileViewer): the
// pushed page's header, rows in grouped cards, Git's letters in the diff colors, empty and failed states.

/**
 * A page of the files screen (iOS's navigation bar here): the round back button, the title and a one-line subtitle
 * (a path keeps its informative end), round buttons at the end. [content] gets the gesture bar's inset to keep clear.
 */
@Composable
internal fun FilesPage(
    title: String,
    onBack: () -> Unit,
    subtitle: String? = null,
    actions: @Composable RowScope.() -> Unit = {},
    content: @Composable (PaddingValues) -> Unit,
) {
    Column(Modifier.fillMaxSize().background(JTheme.colors.page)) {
        PageHeader(title, onBack, subtitle = subtitle?.let { FilesPaths.keepEnd(it, 40) }, actions = actions)
        Box(Modifier.weight(1f).fillMaxWidth()) { content(WindowInsets.navigationBars.asPaddingValues()) }
    }
}

@Composable
internal fun Loading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        CircularProgressIndicator(Modifier.size(JSize.wellXS), strokeWidth = JSize.rule, color = JTheme.colors.secondaryText)
    }
}

/** The design system's empty state in the middle of what is left of the page (empty folders, errors). */
@Composable
internal fun Notice(
    icon: ImageVector,
    title: String,
    detail: String,
    action: String? = null,
    onAction: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        EmptyState(icon, title, detail, action = if (action != null && onAction != null) action to onAction else null)
    }
}

@Composable
internal fun ErrorNotice(title: String, error: String, onRetry: (() -> Unit)?) =
    Notice(JIcons.error, title, error, if (onRetry != null) "重试" else null, onRetry)

/** What to show for a failed call: the hub's message, or a plain network sentence. */
internal fun Throwable.shown(): String = when (this) {
    is HubException -> message
    is IOException -> "连不上 hub：${message ?: javaClass.simpleName}"
    else -> message ?: javaClass.simpleName
}

/** Two or more segments on one row (文件 · Git, 未暂存 · 已暂存), 16 from the edges. */
@Composable
internal fun Segments(titles: List<String>, selected: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    SegmentedControl(titles, selected, onSelect, modifier.padding(horizontal = JSpace.page, vertical = JSpace.s))
}

/** The symbol of a file by its kind (iOS `FileViewer.symbol`): code and config alike are curly braces. */
internal fun fileIcon(name: String, dir: Boolean = false): ImageVector = when {
    dir || name.endsWith("/") -> JIcons.folder
    else -> when (FilesPaths.extension(name)) {
        "md", "markdown", "mdx", "txt", "rtf" -> JIcons.document
        "png", "jpg", "jpeg", "gif", "webp", "heic", "bmp", "svg" -> JIcons.photo
        "pdf" -> JIcons.pdf
        "" -> JIcons.file
        else -> JIcons.code
    }
}

/** Where a listing row's words start (after its symbol): its hairline starts there too. */
internal val FileTextColumn: Dp = JSpace.l + JSize.iconS + JSpace.m

/**
 * One row of a listing (iOS `FilesPane.entryRow`): the kind's symbol (folders in the accent), the name ([quiet]: a
 * hidden one, in the secondary color), its meta line, [trailing] (a Git mark), › on folders.
 */
@Composable
internal fun FileRow(
    name: String, dir: Boolean, onClick: () -> Unit, meta: String? = null, quiet: Boolean = false,
    trailing: @Composable RowScope.() -> Unit = {},
) {
    val c = JTheme.colors
    Row(
        Modifier.fillMaxWidth().heightIn(min = JSize.box50).clickable(onClick = onClick).padding(horizontal = JSpace.l, vertical = JSpace.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(JSpace.m),
    ) {
        Icon(fileIcon(name, dir), null, Modifier.size(JSize.iconS), tint = if (dir) c.accent else c.secondaryText)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xxs)) {
            Text(name, style = JTheme.type.secondary, color = if (quiet) c.secondaryText else c.label, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (!meta.isNullOrEmpty()) Text(meta, style = JTheme.type.label, color = c.secondaryText, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        trailing()
        if (dir) Icon(JIcons.disclosure, null, Modifier.size(JSize.iconXS), tint = c.tertiaryText)
    }
}

/** The color of a Git status letter (as the editors), in the diff colors. */
@Composable
internal fun gitColor(letter: String): Color {
    val d = JTheme.diff
    return when (letter) {
        "A", "U" -> d.add
        "D", "!" -> d.del
        "R", "C" -> JTheme.colors.secondaryText
        else -> d.changed
    }
}

/** The status letter in a small tinted square (iOS `GitBadge`). */
@Composable
internal fun GitBadge(letter: String) {
    val color = gitColor(letter)
    Box(
        Modifier.size(JSize.iconXS).clip(RoundedCornerShape(JRadius.badge)).background(color.copy(alpha = 0.14f)),
        contentAlignment = Alignment.Center,
    ) { Text(letter, style = JTheme.type.labelMonoHeavy, color = color, maxLines = 1) }
}
