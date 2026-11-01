package jarvis.android.ui

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Chat
import androidx.compose.material.icons.filled.Inbox
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.VerticalDivider
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.window.layout.FoldingFeature
import androidx.window.layout.WindowInfoTracker
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map

/** Set around the right pane's page in two panes; null in a single stack (iOS `SplitDetail`). */
data class SplitDetail(val isFirstPage: Boolean)

val LocalSplitDetail = compositionLocalOf<SplitDetail?> { null }

/**
 * The back arrow of a page the list opens: the right pane's first page has none (the list is beside it, iOS
 * `splitDetailPage`). Pages drawn over it (files, the terminal) keep their own arrows.
 */
@Composable
fun PaneBackButton(onBack: () -> Unit) {
    if (LocalSplitDetail.current?.isFirstPage == true) return
    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "返回") }
}

/**
 * A fold that separates the window side by side (a book-style foldable half open), in window dp; null when flat,
 * folded shut, or the fold runs across (tabletop). Jetpack WindowManager reports it per window, so this never reads
 * the physical screen.
 */
@Composable
fun rememberHinge(): Hinge? {
    val activity = LocalContext.current.findActivity() ?: return null
    val density = LocalDensity.current.density
    val flow = remember(activity) {
        runCatching { WindowInfoTracker.getOrCreate(activity).windowLayoutInfo(activity) }.getOrNull()
            ?.map { info ->
                info.displayFeatures.filterIsInstance<FoldingFeature>()
                    .firstOrNull { it.isSeparating && it.orientation == FoldingFeature.Orientation.VERTICAL }
                    ?.bounds
            }
            ?: flowOf(null)
    }
    val bounds by flow.collectAsState(null)
    return bounds?.let { Hinge(it.left / density, it.right / density) }
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}

/**
 * The two panes: [list] on the left, [detail] on the right, a hairline between them — or, over a fold, the fold's
 * width left empty so nothing sits on the hinge.
 */
@Composable
fun TwoPanes(panes: Panes, modifier: Modifier = Modifier, list: @Composable () -> Unit, detail: @Composable () -> Unit) {
    Row(modifier) {
        Box(Modifier.width(panes.listDp.dp).fillMaxHeight()) { list() }
        if (panes.gapDp > SplitLayout.DIVIDER_DP) {
            Spacer(Modifier.width(panes.gapDp.dp).fillMaxHeight().background(MaterialTheme.colorScheme.background))
        } else {
            VerticalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        }
        Box(Modifier.weight(1f).fillMaxHeight()) { detail() }
    }
}

/**
 * Chat and the Jarvis home: at most [SplitLayout.READABLE_DP] wide, centred (iOS `readableWidth`); a phone is narrower,
 * so there it is the full width as before. Top bars stay outside and span the pane.
 */
fun Modifier.readableWidth(): Modifier =
    fillMaxSize().wrapContentWidth().widthIn(max = SplitLayout.READABLE_DP.dp).fillMaxWidth()

/** The right pane before anything is open: a quiet hint (iOS `SplitPlaceholder`, 「选一个 Agent」). */
@Composable
fun SplitPlaceholder(empty: EmptyDetail) {
    val icon = when (empty) {
        EmptyDetail.Agents -> Icons.AutoMirrored.Filled.Chat
        EmptyDetail.Inbox -> Icons.Default.Inbox
        EmptyDetail.Settings -> Icons.Default.Settings
    }
    // A Surface sets the content colour; a plain background left the title black on the dark page.
    Surface(Modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background, contentColor = MaterialTheme.colorScheme.onBackground) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center, modifier = Modifier.padding(32.dp)) {
                Icon(icon, null, Modifier.size(44.dp), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.height(12.dp))
                Text(empty.title, style = MaterialTheme.typography.titleMedium, maxLines = 1)
                Spacer(Modifier.height(4.dp))
                Text(
                    empty.message, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant,
                    textAlign = TextAlign.Center,
                )
            }
        }
    }
}
