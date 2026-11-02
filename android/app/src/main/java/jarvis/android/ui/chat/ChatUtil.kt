package jarvis.android.ui.chat

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.asPaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.InsertDriveFile
import androidx.compose.material.icons.rounded.Code
import androidx.compose.material.icons.rounded.Folder
import androidx.compose.material.icons.rounded.Image
import androidx.compose.material.icons.rounded.PictureAsPdf
import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.produceState
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import jarvis.android.data.HubException
import jarvis.android.ui.design.PageScaffold
import jarvis.android.ui.ContextRef
import jarvis.android.ui.composeWithContexts
import kotlinx.coroutines.delay
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/** One line of Chinese for a failed request. */
fun Throwable.shownText(): String = when (this) {
    is HubException -> message
    is java.io.IOException -> "连不上 hub：${message ?: javaClass.simpleName}"
    else -> message ?: "出错了"
}

fun Throwable.isCancellation(): Boolean = this is kotlinx.coroutines.CancellationException

/** 「12 KB」「3.4 MB」 (decimal units, like iOS `ByteCountFormatter`). */
fun formatBytes(n: Long): String = when {
    n >= 1_000_000_000 -> String.format(Locale.ROOT, "%.1f GB", n / 1e9)
    n >= 1_000_000 -> String.format(Locale.ROOT, "%.1f MB", n / 1e6)
    n >= 1_000 -> "${n / 1000} KB"
    else -> "$n 字节"
}

/** What a message says when only pictures or excerpts are sent (iOS `Composer.sendText`). */
fun composeMessage(typed: String, hasImages: Boolean, refs: List<ContextRef>): String? {
    val t = typed.trim()
    if (t.isEmpty() && !hasImages && refs.isEmpty()) return null
    val body = t.ifEmpty { if (hasImages) "请看图片" else "参考上面的内容。" }
    return composeWithContexts(body, refs)
}

/** The message quoted at the start of the input box (「引用」): every line behind `> `. */
fun quoted(message: String, draft: String): String {
    val q = message.trim().lines().joinToString("\n") { "> $it" }
    return if (draft.isBlank()) "$q\n\n" else "$q\n\n$draft"
}

/** 「刚刚」「5 分钟前」「3 小时前」「昨天 17:05」「10月3日」. */
fun relativeTime(t: Instant, now: Long = System.currentTimeMillis()): String {
    val s = Duration.between(t, Instant.ofEpochMilli(now)).seconds
    val z = ZoneId.systemDefault()
    return when {
        s < 60 -> "刚刚"
        s < 3600 -> "${s / 60} 分钟前"
        s < 86_400 -> "${s / 3600} 小时前"
        s < 2 * 86_400 -> "昨天 " + DateTimeFormatter.ofPattern("HH:mm").withZone(z).format(t)
        else -> DateTimeFormatter.ofPattern("M月d日").withZone(z).format(t)
    }
}

/** The current time, ticking every [periodMs] while composed. */
@Composable
fun rememberNow(periodMs: Long): State<Long> = produceState(System.currentTimeMillis(), periodMs) {
    while (true) {
        delay(periodMs)
        value = System.currentTimeMillis()
    }
}

/** An icon for a path by its extension. */
fun fileIcon(path: String): ImageVector {
    if (path.endsWith("/")) return Icons.Rounded.Folder
    return when (path.substringAfterLast('.', "").lowercase()) {
        "png", "jpg", "jpeg", "gif", "webp", "heic", "svg" -> Icons.Rounded.Image
        "pdf" -> Icons.Rounded.PictureAsPdf
        "kt", "kts", "swift", "go", "py", "js", "ts", "tsx", "jsx", "java", "rs", "c", "h", "cpp", "sh", "rb", "json", "yaml", "yml", "toml",
        "html", "css", "sql" -> Icons.Rounded.Code
        else -> Icons.AutoMirrored.Rounded.InsertDriveFile
    }
}

/**
 * A full-screen page in a dialog (tool output, selectable text, the plan): the design system's page header — the
 * round back button, the title, [actions] at the end — then [content], given the gesture bar's inset to keep clear.
 */
@Composable
fun FullScreenPage(
    title: String,
    onBack: () -> Unit,
    actions: @Composable () -> Unit = {},
    content: @Composable (PaddingValues) -> Unit,
) {
    Dialog(onDismissRequest = onBack, properties = DialogProperties(usePlatformDefaultWidth = false, decorFitsSystemWindows = false)) {
        FullScreenPageBody(title, onBack, actions, content)
    }
}

/** What [FullScreenPage] shows in its dialog (screenshots draw it without one). */
@Composable
internal fun FullScreenPageBody(title: String, onBack: () -> Unit, actions: @Composable () -> Unit, content: @Composable (PaddingValues) -> Unit) {
    PageScaffold(title, onBack, actions = { actions() }) {
        Box(Modifier.weight(1f).fillMaxWidth()) { content(WindowInsets.navigationBars.asPaddingValues()) }
    }
}
