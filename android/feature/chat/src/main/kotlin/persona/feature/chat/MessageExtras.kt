package persona.feature.chat

import androidx.compose.runtime.Composable
import androidx.compose.runtime.State
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import persona.core.contracts.Item
import persona.core.contracts.Session
import persona.core.designsystem.JChip
import persona.core.designsystem.JTextButton
import persona.core.designsystem.icons.PhIcons
import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

/** The words the hub adds where it cut a long text for the page (`…（已截断，共 N 字节）`). */
private const val CUT_MARK = "\n…（已截断"

/** [text] without the hub's cut mark: the part a page carries of a text with `full_size`. */
internal fun cutText(text: String): String {
    val at = text.lastIndexOf(CUT_MARK)
    return if (at >= 0) text.substring(0, at) else text
}

/** 「12 KB」「3.4 MB」: decimal units, like iOS `ByteCountFormatter`. */
internal fun formatBytes(bytes: Long): String = when {
    bytes >= 1_000_000_000 -> String.format(Locale.ROOT, "%.1f GB", bytes / 1e9)
    bytes >= 1_000_000 -> String.format(Locale.ROOT, "%.1f MB", bytes / 1e6)
    bytes >= 1_000 -> "${bytes / 1000} KB"
    else -> "$bytes 字节"
}

/** 「刚刚」「5 分钟前」「3 小时前」「昨天 17:05」「10月3日」. */
internal fun relativeTime(time: Instant, now: Instant, zone: ZoneId = ZoneId.systemDefault()): String {
    val seconds = Duration.between(time, now).seconds.coerceAtLeast(0)
    return when {
        seconds < 60 -> "刚刚"
        seconds < 3600 -> "${seconds / 60} 分钟前"
        seconds < 86_400 -> "${seconds / 3600} 小时前"
        seconds < 2 * 86_400 -> "昨天 " + DateTimeFormatter.ofPattern("HH:mm", Locale.ROOT).withZone(zone).format(time)
        else -> DateTimeFormatter.ofPattern("M月d日", Locale.ROOT).withZone(zone).format(time)
    }
}

/** The current time, ticking every [periodMillis] while composed. */
@Composable
internal fun rememberNow(periodMillis: Long, enabled: Boolean = true): State<Instant> = produceState(Instant.now(), periodMillis, enabled) {
    while (enabled) {
        delay(periodMillis)
        value = Instant.now()
    }
}

/**
 * 「看全文（12 KB）」 under a text the hub cut (`full_size`): loads the whole text and hands it to [onLoaded], which
 * shows it in place of the cut one; 「加载中…」 meanwhile, 「加载失败，点一下重试」 when it fails.
 */
@Composable
internal fun FullTextButton(item: Item, onLoaded: (String) -> Unit) {
    val actions = LocalChatRowActions.current ?: return
    val size = item.fullSize ?: return
    val scope = rememberCoroutineScope()
    var loading by remember(item.id) { mutableStateOf(false) }
    var failed by remember(item.id) { mutableStateOf(false) }
    JTextButton(
        when {
            loading -> "加载中…"
            failed -> "加载失败，点一下重试"
            else -> "看全文（${formatBytes(size.toLong())}）"
        },
        {
            loading = true
            failed = false
            scope.launch {
                try {
                    onLoaded(actions.fullText(item))
                } catch (e: CancellationException) {
                    throw e
                } catch (_: Exception) {
                    failed = true
                } finally {
                    loading = false
                }
            }
        },
        enabled = !loading, icon = if (failed) PhIcons.ArrowClockwise else PhIcons.ArrowSquareOut,
    )
}

/**
 * The next message Claude suggests (X1, grey in its own input box): a chip over the empty composer of an idle session;
 * a tap puts it in the composer to edit or send.
 */
@Composable
internal fun ComposerSuggestion(session: Session?, composer: ChatComposerState) {
    val suggestion = suggestionFor(session, composer.hasContent) ?: return
    JChip(suggestion, onClick = { composer.editMessage(suggestion); composer.focusRequest++ }, icon = PhIcons.Sparkle)
}

/** The suggestion to offer: the session's, while it is idle and nothing is typed yet. */
internal fun suggestionFor(session: Session?, typed: Boolean): String? =
    session?.suggestion?.trim()?.takeIf { it.isNotEmpty() && !typed && session?.status != "working" }
