package jarvis.android.ui.files

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.outlined.InsertDriveFile
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.DataObject
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Folder
import androidx.compose.material.icons.outlined.Image
import androidx.compose.material.icons.outlined.PictureAsPdf
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.io.IOException
import jarvis.android.data.HubException

// Shared pieces of the files screen's pages (master's plain Material3 style, as SpaceScreen).

/** A page with a back arrow, a title (and a one-line subtitle), [actions] on the right. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun FilesPage(
    title: String,
    onBack: () -> Unit,
    subtitle: String? = null,
    actions: @Composable RowScope.() -> Unit = {},
    content: @Composable (PaddingValues) -> Unit,
) {
    Scaffold(
        topBar = {
            TopAppBar(
                navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") } },
                title = {
                    Column {
                        Text(title, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        if (!subtitle.isNullOrEmpty()) {
                            Text(
                                FilesPaths.keepEnd(subtitle, 40), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant,
                                maxLines = 1, overflow = TextOverflow.Ellipsis,
                            )
                        }
                    }
                },
                actions = actions,
            )
        },
        content = content,
    )
}

@Composable
internal fun Loading(modifier: Modifier = Modifier) {
    Box(modifier.fillMaxSize(), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(28.dp), strokeWidth = 2.dp) }
}

/** An icon, a title, an optional detail and an optional action, centered (empty folders, errors). */
@Composable
internal fun Notice(
    icon: ImageVector,
    title: String,
    detail: String? = null,
    action: String? = null,
    onAction: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier.fillMaxSize().padding(32.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp, Alignment.CenterVertically),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(icon, null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(40.dp))
        Text(title, style = MaterialTheme.typography.titleMedium, textAlign = TextAlign.Center)
        if (!detail.isNullOrEmpty()) {
            Text(detail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        }
        if (action != null && onAction != null) TextButton(onClick = onAction) { Text(action, maxLines = 1) }
    }
}

@Composable
internal fun ErrorNotice(title: String, error: String, onRetry: (() -> Unit)?) =
    Notice(Icons.Outlined.ErrorOutline, title, error, if (onRetry != null) "重试" else null, onRetry)

/** What to show for a failed call: the hub's message, or a plain network sentence. */
internal fun Throwable.shown(): String = when (this) {
    is HubException -> message
    is IOException -> "连不上 hub：${message ?: javaClass.simpleName}"
    else -> message ?: javaClass.simpleName
}

/** Two or more segments on one row (文件 · Git, 未暂存 · 已暂存). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun Segments(titles: List<String>, selected: Int, onSelect: (Int) -> Unit, modifier: Modifier = Modifier) {
    SingleChoiceSegmentedButtonRow(modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        titles.forEachIndexed { i, t ->
            SegmentedButton(
                selected = i == selected,
                onClick = { onSelect(i) },
                shape = SegmentedButtonDefaults.itemShape(i, titles.size),
                icon = {},
                label = { Text(t, maxLines = 1, overflow = TextOverflow.Ellipsis) },
            )
        }
    }
}

internal fun fileIcon(name: String, dir: Boolean = false): ImageVector = when {
    dir || name.endsWith("/") -> Icons.Outlined.Folder
    else -> when (FilesPaths.extension(name)) {
        "md", "markdown", "mdx", "txt", "rtf" -> Icons.Outlined.Description
        "png", "jpg", "jpeg", "gif", "webp", "heic", "bmp", "svg" -> Icons.Outlined.Image
        "pdf" -> Icons.Outlined.PictureAsPdf
        "json", "yml", "yaml", "toml", "plist", "xml", "ini" -> Icons.Outlined.DataObject
        "" -> Icons.AutoMirrored.Outlined.InsertDriveFile
        else -> Icons.Outlined.Code
    }
}

/** The color of a Git status letter (as the editors). */
@Composable
internal fun gitColor(letter: String): Color {
    val c = diffColors()
    return when (letter) {
        "A", "U" -> c.add
        "D", "!" -> c.del
        "R", "C" -> MaterialTheme.colorScheme.onSurfaceVariant
        else -> c.changed
    }
}

/** The status letter in a small tinted square. */
@Composable
internal fun GitBadge(letter: String) {
    val color = gitColor(letter)
    Box(Modifier.size(20.dp).background(color.copy(alpha = 0.14f), RoundedCornerShape(5.dp)), contentAlignment = Alignment.Center) {
        Text(letter, fontSize = 11.sp, fontWeight = FontWeight.Bold, fontFamily = FontFamily.Monospace, color = color, maxLines = 1)
    }
}

/** A small one-line label (branch / tag names, counts). */
@Composable
internal fun Tag(text: String, color: Color = MaterialTheme.colorScheme.onSurfaceVariant, modifier: Modifier = Modifier) {
    Text(
        text, fontSize = 11.sp, color = color, maxLines = 1,
        modifier = modifier.background(color.copy(alpha = 0.12f), RoundedCornerShape(50)).padding(horizontal = 7.dp, vertical = 2.dp),
    )
}

@Composable
internal fun SectionTitle(text: String, modifier: Modifier = Modifier) {
    Text(
        text, style = MaterialTheme.typography.labelLarge, color = MaterialTheme.colorScheme.primary,
        modifier = modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 6.dp), maxLines = 1,
    )
}
