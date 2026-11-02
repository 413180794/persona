package jarvis.android.ui.chat

import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.JTheme
import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.KeyboardArrowRight
import androidx.compose.material.icons.rounded.AutoAwesome
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.Error
import androidx.compose.material.icons.rounded.FormatQuote
import androidx.compose.material.icons.rounded.History
import androidx.compose.material.icons.rounded.Notifications
import androidx.compose.material.icons.rounded.Psychology
import androidx.compose.material.icons.rounded.Schedule
import androidx.compose.material.icons.rounded.SwapHoriz
import androidx.compose.material.icons.rounded.Terminal
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import jarvis.android.data.*
import jarvis.android.ui.*
import jarvis.android.ui.media.*
import jarvis.android.ui.files.DiffText
import jarvis.android.ui.files.DiffLine
import jarvis.android.ui.files.DiffContent
import jarvis.android.ui.files.readFile
import jarvis.android.chat.Outbox
import jarvis.android.chat.PendingMessage
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.runtime.ReadOnlyComposable
import kotlinx.coroutines.launch

// The chat's message rows (apple/Jarvis/Views/Chat/MessageViews.swift). Tools are in chat/ToolViews.kt.

// --- User ------------------------------------------------------------------------------------------------------------

/** Your message: excerpt cards and photos above, the bubble, file chips under it (iOS `UserBubble`). */
@Composable
fun UserBubble(item: Item) {
    val parsed = remember(item.id) { userText(item) }
    val from = parsed.from ?: item.from
    if (from != null) {
        PeerMessage(item, parsed.text, from)
        return
    }
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        if (parsed.refs.isNotEmpty()) ContextCards(parsed.refs)
        if (parsed.images.isNotEmpty()) AttachedImages(parsed.images)
        if (parsed.text.isNotEmpty()) Bubble(parsed.text, Modifier.messageLongPress(item.id, parsed.text, isUser = true))
        FileChips(parsed.files, alignEnd = true)
    }
}

/** The colored bubble of your side (at most the width minus 48). */
@Composable
fun Bubble(text: String, modifier: Modifier = Modifier, faded: Boolean = false, fill: Boolean = true) {
    val c = chatColors
    // [fill] = false: as wide as the text (a status icon sits right beside it in a row).
    // Bare URLs in what you typed open in the app's link sheet (CLAUDE.md「链接在 App 内打开」), underlined in the
    // bubble's text colour so they read on any style's bubble.
    val from = jarvis.android.ui.web.LocalLinkSource.current
    val shown = remember(text, c.userBubbleText, from) {
        jarvis.android.ui.linkified(text, c.userBubbleText, underline = true) { jarvis.android.ui.web.Links.open(it, from) }
    }
    BoxWithConstraints(if (fill) Modifier.fillMaxWidth() else Modifier, contentAlignment = Alignment.CenterEnd) {
        Text(
            shown,
            style = MaterialTheme.typography.bodyLarge,
            color = c.userBubbleText,
            modifier = modifier.widthIn(max = maxWidth - 48.dp)
                .background(c.userBubble.copy(alpha = if (faded) 0.6f else 1f), RoundedCornerShape(JRadius.bubble))
                .padding(horizontal = JSpace.l, vertical = JSpace.m),
        )
    }
}

/** A message another Claude session sent this one: on the left in a quiet card naming the sender. */
@Composable
private fun PeerMessage(item: Item, text: String, from: String) {
    val c = chatColors
    Column(
        Modifier.padding(end = JSpace.xxl).fillMaxWidth().clip(RoundedCornerShape(JRadius.card)).background(c.fillWell)
            .border(0.5.dp, c.label.copy(alpha = 0.08f), RoundedCornerShape(JRadius.card))
            .messageLongPress(item.id, text, isUser = false).padding(JSpace.m),
        verticalArrangement = Arrangement.spacedBy(JSpace.sm),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Icon(JIcons.fromPeer, null, Modifier.size(JSize.markS), tint = c.secondaryLabel)
            Text("来自 $from", style = JTheme.type.labelStrong, color = c.secondaryLabel, maxLines = 1)
        }
        Text(text, style = JTheme.type.body, color = c.label)
    }
}

/** `<jarvis-context>` excerpts a message carries: quote cards; a tap opens the page they came from. */
@Composable
fun ContextCards(refs: List<ContextRef>) {
    val c = chatColors
    val from = jarvis.android.ui.web.LocalLinkSource.current // the page opens in the link sheet, not a browser
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        refs.forEach { r ->
            Row(
                Modifier.widthIn(max = JSize.box300).clip(RoundedCornerShape(JRadius.card)).background(c.fillWell)
                    .clickable(enabled = r.url.startsWith("http")) { jarvis.android.ui.web.Links.open(r.url, from) }.height(IntrinsicSize.Min),
            ) {
                Box(Modifier.padding(vertical = JSpace.s).width(JSpace.xxs).fillMaxHeight().background(c.accent.copy(alpha = 0.5f), CircleShape))
                Column(Modifier.padding(JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                        Icon(if (r.kind == "edit") JIcons.ai else JIcons.quote, null, Modifier.size(JSize.markXS), tint = c.accent)
                        Text((if (r.kind == "edit") "要改的内容 · " else "上下文 · ") + r.title, style = JTheme.type.labelStrong,
                            color = c.accent, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    Text(r.text, style = JTheme.type.caption, color = c.secondaryLabel, maxLines = 3, overflow = TextOverflow.Ellipsis)
                    val host = remember(r.url) { runCatching { java.net.URI(r.url).host }.getOrNull().orEmpty() }
                    if (host.isNotEmpty()) Text(host, style = JTheme.type.label, color = c.tertiaryLabel, maxLines = 1)
                }
            }
        }
    }
}

/** Photos you sent with a message: square thumbnails above it, read from the session's machine (iOS `AttachedImages`). */
@Composable
private fun AttachedImages(paths: List<String>) {
    val env = LocalChatEnv.current ?: return
    val side = if (paths.size == 1) 120.dp else 76.dp
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(JSpace.sm, Alignment.End)) {
        val loaded = paths.map { path ->
            produceState<ViewerImage?>(AttachedImageCache.get(env.key.machineId, path), path) {
                if (value != null) return@produceState
                val f = runCatching { env.hub.api.readFile(env.key.machineId, path, env.cwd) }.getOrNull()
                if (f?.data != null && f.mime.orEmpty().startsWith("image/")) {
                    value = ViewerImage(data = f.data, mime = f.mime).also { AttachedImageCache.put(env.key.machineId, path, it) }
                }
            }
        }
        loaded.forEachIndexed { i, st ->
            val img = st.value
            if (img != null) {
                SquareThumb(img, side) { env.openImage(img, loaded.mapNotNull { it.value }) }
            } else {
                Box(Modifier.size(side).clip(RoundedCornerShape(JRadius.control)).background(chatColors.fillChip), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(Modifier.size(JSize.markM), strokeWidth = JSize.rule)
                }
            }
        }
    }
}

/** Photos read from machines survive scrolling a lazy list (rows are rebuilt when they come back on screen). */
private object AttachedImageCache {
    private val map = object : LinkedHashMap<String, ViewerImage>(32, 0.75f, true) {
        override fun removeEldestEntry(eldest: MutableMap.MutableEntry<String, ViewerImage>?) = size > 40
    }
    @Synchronized fun get(m: String, p: String) = map["$m:$p"]
    @Synchronized fun put(m: String, p: String, v: ViewerImage) { map["$m:$p"] = v }
}

/** Photos that came with your message as transcript images: square thumbnails on your side (iOS `SentPhotos`). */
@Composable
fun SentPhotos(items: List<Item>, key: SessionKey) {
    val env = LocalChatEnv.current
    val imgs = remember(items) { items.map { imageOf(key, it) } }
    val side = if (imgs.size == 1) 140.dp else 84.dp
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(JSpace.sm, Alignment.End)) {
        imgs.forEach { img -> SquareThumb(img, side) { env?.openImage(img, imgs) } }
    }
}

/** Paths found in a message: chips that open the file on the session's machine. */
@Composable
fun FileChips(paths: List<String>, alignEnd: Boolean = false) {
    val env = LocalChatEnv.current
    if (paths.isEmpty() || env?.canOpenFiles != true) return
    val c = chatColors
    Row(
        Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()),
        horizontalArrangement = Arrangement.spacedBy(JSpace.sm, if (alignEnd) Alignment.End else Alignment.Start),
    ) {
        paths.forEach { p ->
            Row(
                Modifier.clip(CircleShape).background(c.accent.copy(alpha = 0.1f)).clickable { env.openFile(p) }.padding(horizontal = JSpace.s, vertical = JSpace.xs),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(JSpace.xs),
            ) {
                Icon(fileIcon(p), null, Modifier.size(JSize.markXS), tint = c.accent)
                Text(p.trimEnd('/').substringAfterLast('/').ifEmpty { p }, style = JTheme.type.labelMedium, color = c.accent, maxLines = 1)
            }
        }
    }
}

/**
 * A message just sent from this phone, before the transcript has it (iOS `PendingBubble`): drawn like a sent bubble
 * at once, a quiet clock while it goes, a red mark (重发 / 删除) when it failed.
 */
@Composable
fun PendingBubble(p: PendingMessage, key: SessionKey) {
    val hub = LocalChatEnv.current?.hub
    val c = chatColors
    val bad = p.phase == PendingMessage.Phase.FAILED || p.phase == PendingMessage.Phase.REFUSED || p.phase == PendingMessage.Phase.UNKNOWN
    var menu by remember { mutableStateOf(false) }
    val parsed = remember(p.text) { parseContexts(p.text) }
    val text = parsed.second
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        if (parsed.first.isNotEmpty()) ContextCards(parsed.first)
        if (p.images.isNotEmpty()) {
            val side = if (p.images.size == 1) 120.dp else 76.dp
            Row(horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                p.images.forEach { SquareThumb(remember(it) { ViewerImage(data = it.data, mime = it.mime) }, side, null) }
            }
        }
        Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
            when {
                bad -> Box {
                    Icon(
                        JIcons.critical, contentDescription = "发送失败，重发或删除", tint = c.danger,
                        modifier = Modifier.padding(bottom = JSpace.m).size(JSize.iconS).clip(CircleShape).clickable { menu = true },
                    )
                    DropdownMenu(menu, onDismissRequest = { menu = false }) {
                        if (p.canClearDraft) DropdownMenuItem(text = { Text("清空并发送", maxLines = 1) },
                            onClick = { menu = false; hub?.let { Outbox.retry(it, key, p.clientMsgId, clearDraft = true) } })
                        DropdownMenuItem(text = { Text(if (p.phase == PendingMessage.Phase.UNKNOWN) "再发一次" else "重发", maxLines = 1) },
                            onClick = { menu = false; hub?.let { Outbox.retry(it, key, p.clientMsgId) } })
                        DropdownMenuItem(text = { Text("删除", color = c.danger, maxLines = 1) },
                            onClick = { menu = false; Outbox.discard(key, p.clientMsgId) })
                    }
                }
                p.phase == PendingMessage.Phase.SENDING ->
                    Icon(JIcons.time, contentDescription = "发送中", tint = c.tertiaryLabel, modifier = Modifier.padding(bottom = JSpace.m).size(JSize.markXS))
                // Written offline: a clock that offers 现在重试 / 不发了 (iOS Outbox).
                p.phase == PendingMessage.Phase.WAITING -> Box {
                    Icon(
                        JIcons.time, contentDescription = "连上后自动发送，点按可以现在重试或不发了", tint = c.secondaryLabel,
                        modifier = Modifier.padding(bottom = JSpace.m).size(JSize.iconS).clip(CircleShape).clickable { menu = true }.padding(JSpace.xxs),
                    )
                    DropdownMenu(menu, onDismissRequest = { menu = false }) {
                        DropdownMenuItem(text = { Text("现在重试", maxLines = 1) },
                            onClick = { menu = false; hub?.let { Outbox.sendNow(it, key, p.clientMsgId) } })
                        DropdownMenuItem(text = { Text("不发了", color = c.danger, maxLines = 1) },
                            onClick = { menu = false; Outbox.discard(key, p.clientMsgId) })
                    }
                }
                else -> Unit
            }
            Box(Modifier.weight(1f, fill = false)) { Bubble(text.ifEmpty { "图片" }, faded = bad, fill = false) }
        }
        if (p.phase == PendingMessage.Phase.WAITING) {
            Text("连上后自动发送", style = JTheme.type.label, color = c.secondaryLabel, textAlign = TextAlign.End, maxLines = 1)
        }
        if (bad) {
            Text(
                when (p.phase) {
                    // The hub's own error text is English; say it in the app's words.
                    PendingMessage.Phase.REFUSED -> if (p.fields["code"] == "dialog_open") "电脑上有对话框，没有发送" else "电脑上的输入框里有内容，没有发送"
                    PendingMessage.Phase.UNKNOWN -> "可能已经送到电脑上了：先看看对话里有没有，再决定要不要重发"
                    else -> p.error ?: "没发出去"
                },
                style = JTheme.type.label,
                color = c.danger,
                textAlign = TextAlign.End,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = JSize.avatarXL),
            )
        }
    }
}

// --- Assistant -------------------------------------------------------------------------------------------------------

/** The agent's words: Markdown, no bubble, reading first; file chips; 看全文 when the hub cut it. */
@Composable
fun AssistantText(item: Item, modifier: Modifier = Modifier, quiet: Boolean = false) {
    FullTextLoader(item) { text ->
        Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            // Quiet: what it said in the middle of its work (the timeline), a size down and gray (iOS `AssistantText(quiet:)`).
            Markdown(text, Modifier.fillMaxWidth().messageLongPress(item.id, text, isUser = false), selectable = false,
                color = if (quiet) chatColors.secondaryLabel else chatColors.label, base = if (quiet) JTheme.type.secondary else JTheme.type.body)
            FileChips(remember(text) { FilePaths.find(text) })
        }
    }
}

/** Shows [Item.cutText] and 「看全文」 when the hub cut the text; swaps in the whole text once loaded. */
@Composable
private fun FullTextLoader(item: Item, content: @Composable (String) -> Unit) {
    val env = LocalChatEnv.current
    var full by remember(item.id) { mutableStateOf<String?>(null) }
    var loading by remember(item.id) { mutableStateOf(false) }
    var failed by remember(item.id) { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val c = chatColors
    Column {
        content(full ?: item.cutText)
        if (item.fullSize != null && full == null && env != null) {
            TextButton(
                onClick = {
                    loading = true
                    failed = false
                    scope.launch {
                        full = runCatching { env.hub.api.fullItem(env.key, item.id).text }.getOrNull()
                        failed = full == null
                        loading = false
                    }
                },
                enabled = !loading,
            ) {
                Text(
                    when {
                        loading -> "加载中…"
                        failed -> "加载失败，点一下重试"
                        else -> "看全文（${formatBytes(item.fullSize.toLong())}）"
                    },
                    color = if (failed && !loading) c.danger else c.accent,
                    maxLines = 1,
                )
            }
        }
    }
}

/** Thinking: one quiet line (its first line); tap for all of it (iOS `ThinkingRow`). */
@Composable
fun ThinkingRow(item: Item, modifier: Modifier = Modifier) {
    var open by remember(item.id) { mutableStateOf(false) }
    val c = chatColors
    val text = item.text.orEmpty().trim()
    Column(modifier.fillMaxWidth().animateContentSize(), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        Row(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.badge)).clickable { open = !open }.padding(vertical = JSpace.xxs),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
        ) {
            Icon(JIcons.thinking, null, Modifier.size(JSize.markS), tint = c.secondaryLabel)
            Text(if (open) "思考过程" else ToolText.firstLine(text), style = JTheme.type.label, color = c.secondaryLabel, maxLines = 1,
                overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            Icon(JIcons.disclosure, null, Modifier.size(JSize.markS).rotate(if (open) 90f else 0f), tint = c.secondaryLabel)
        }
        if (open) {
            Row(Modifier.height(IntrinsicSize.Min)) {
                Box(Modifier.width(JSpace.xxs).fillMaxHeight().background(c.secondaryLabel.copy(alpha = 0.3f)))
                SelectionContainer {
                    Text(text, style = JTheme.type.caption, color = c.secondaryLabel, modifier = Modifier.padding(start = JSpace.m))
                }
            }
        }
    }
}

/** A system notice: a quiet capsule; a background task's report says what finished (iOS `NoticeRow`). */
@Composable
fun NoticeRow(item: Item, inWork: Boolean = false) {
    val c = chatColors
    val text = item.text.orEmpty().trim()
    Box(Modifier.fillMaxWidth(), contentAlignment = if (inWork) Alignment.CenterStart else Alignment.Center) {
        if (item.wake == true) {
            val (summary, failed) = remember(text) { wakeSummary(text) }
            Row(
                Modifier.clip(CircleShape).background(c.fillWell).padding(horizontal = JSpace.m, vertical = JSpace.sm),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
            ) {
                Icon(if (failed) JIcons.critical else JIcons.notifications, null, Modifier.size(JSize.markXS), tint = if (failed) c.danger else c.accent)
                Text(summary, style = JTheme.type.label, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        } else {
            Text(
                text, style = JTheme.type.label, color = c.secondaryLabel, textAlign = TextAlign.Center, maxLines = 4, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.clip(RoundedCornerShape(JRadius.card)).background(c.fillWell).padding(horizontal = JSpace.m, vertical = JSpace.xs),
            )
        }
    }
}

// --- Away recap --------------------------------------------------------------------------------------------------------

/** Claude Code's "while you were away" recap: one short line (「↺ 离开期间回顾 · 21 分钟前 ›」); the recap opens in a drawer. */
@Composable
fun RecapCard(item: Item) {
    var open by remember(item.id) { mutableStateOf(false) }
    val c = chatColors
    val now by rememberNow(60_000)
    Box(Modifier.fillMaxWidth()) {
        Row(
            Modifier.clip(CircleShape).background(c.fillWell).clickable { open = true }.padding(horizontal = JSpace.m, vertical = JSpace.xs),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
        ) {
            Icon(JIcons.recent, null, Modifier.size(JSize.mark), tint = c.accent)
            Text("离开期间回顾" + (item.time?.let { " · " + relativeTime(it, now) } ?: ""), style = JTheme.type.caption, color = c.secondaryLabel, maxLines = 1)
            Icon(JIcons.disclosure, null, Modifier.size(JSize.markS), tint = c.secondaryLabel)
        }
    }
    if (open) RecapDrawer(item) { open = false }
}

/** The recap in full, in a drawer from the bottom (iOS `RecapDrawer`). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RecapDrawer(item: Item, onDismiss: () -> Unit) {
    val c = chatColors
    val now by rememberNow(60_000)
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(), containerColor = c.page) {
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = JSpace.l).navigationBarsPadding().padding(bottom = JSpace.l),
            verticalArrangement = Arrangement.spacedBy(JSpace.m)) {
            Text("离开期间回顾", style = JTheme.type.heading, color = c.label, modifier = Modifier.align(Alignment.CenterHorizontally))
            item.time?.let { Text("写于 " + relativeTime(it, now), style = JTheme.type.caption, color = c.secondaryLabel) }
            SelectionContainer { Markdown(item.text.orEmpty(), Modifier.fillMaxWidth()) }
        }
    }
}

// --- Work lines ------------------------------------------------------------------------------------------------------

/**
 * The quiet line under a message (iOS `WorkLine`): 「工具 5 · 思考 1 · 回复 1 · 4 分 17 秒 ›」; the running one has the
 * working spinner, ticks, and the terminal at the end. Never cut: a shorter label when the full one does not fit.
 */
@Composable
fun WorkLineRow(w: FoldedRow.Work, onToggle: () -> Unit, onTerminal: (() -> Unit)?) {
    val c = chatColors
    val now by rememberNow(if (w.running) 1_000 else 60_000)
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.SpaceBetween) {
        Row(
            Modifier.weight(1f, fill = false).clip(RoundedCornerShape(JRadius.badge)).clickable(enabled = w.items.isNotEmpty(), onClick = onToggle)
                .padding(vertical = JSpace.xs, horizontal = JSpace.xxs),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
        ) {
            if (w.running) CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium, color = chatColors.working)
            BoxWithConstraints(Modifier.weight(1f, fill = false)) {
                val full = workLabel(w.stats, w.running, now)
                val measurer = androidx.compose.ui.text.rememberTextMeasurer()
                val style = JTheme.type.caption
                val width = remember(full, measurer, style) {
                    measurer.measure(full, style, softWrap = false, maxLines = 1).size.width
                }
                val tooLong = width > constraints.maxWidth
                Text(if (tooLong) workLabel(w.stats, w.running, now, short = true) else full, style = JTheme.type.caption, color = c.secondaryLabel,
                    maxLines = 1, softWrap = false)
            }
            if (w.items.isNotEmpty()) {
                Icon(JIcons.disclosure, if (w.expanded) "收起过程" else "展开过程",
                    Modifier.size(JSize.markS).rotate(if (w.expanded) 90f else 0f), tint = c.secondaryLabel)
            }
        }
        if (onTerminal != null) {
            Spacer(Modifier.width(JSpace.s))
            Icon(JIcons.terminal, contentDescription = "打开终端", tint = c.secondaryLabel,
                modifier = Modifier.size(JSize.well).clip(CircleShape).clickable(onClick = onTerminal).padding(JSpace.s))
        }
    }
}

/** Half the chat list's spacing: a slice reaches that far into the gap to meet its neighbours. */
val WorkBridge: Dp = 6.dp

/**
 * 液态玻璃 lays an open work line out as a timeline (iOS `StyleSpec.workList = .timeline`, 2026-10-31): no card, a thin
 * line down the left from the first step to 「收起 N 步」, the tools' round symbols sitting on it; the other styles
 * keep the card.
 */
val workTimeline: Boolean
    @Composable @ReadOnlyComposable get() = JTheme.style == AppStyle.GLASS

/** Where the timeline's line runs: under the tools' symbols. */
val WorkLineX: Dp = WorkInset + JSize.icon / 2

/**
 * A slice of an open work line (iOS `WorkCardSlice`): each step is a lazy list line of its own, so each draws its
 * part — of the card (rounded on top for the first, at the bottom for the 收起 foot, a hairline from the words'
 * column between steps) or of the timeline's line — reaching into the list's gaps to meet its neighbours.
 */
@Composable
fun Modifier.workSlice(top: Boolean, bottom: Boolean): Modifier {
    val c = chatColors
    if (workTimeline) {
        val line = c.separator
        return this.drawBehind {
            val bridge = WorkBridge.toPx()
            val x = WorkLineX.toPx()
            val y0 = if (top) JSize.tapTarget.toPx() / 2 else -bridge
            val y1 = if (bottom) size.height / 2 else size.height + bridge
            drawLine(line, Offset(x, y0), Offset(x, y1), strokeWidth = JSize.lineMedium.toPx())
        }
    }
    val fill = c.fillWell
    val stroke = c.cardStroke
    val sep = c.separator
    return this.drawBehind {
        val bridge = WorkBridge.toPx()
        val r = JRadius.card.toPx()
        val y0 = if (top) 0f else -bridge
        val y1 = if (bottom) size.height else size.height + bridge
        val rect = Rect(0f, y0, size.width, y1)
        val cr = CornerRadius(r)
        val rr = RoundRect(rect, topLeft = if (top) cr else CornerRadius.Zero, topRight = if (top) cr else CornerRadius.Zero,
            bottomLeft = if (bottom) cr else CornerRadius.Zero, bottomRight = if (bottom) cr else CornerRadius.Zero)
        drawPath(Path().apply { addRoundRect(rr) }, fill)
        // The outline: the sides, plus the rounded top / bottom where the card starts / ends.
        val w = JSize.hairline.toPx()
        val p = Path()
        p.moveTo(0f, if (bottom) y1 - r else y1)
        if (top) {
            p.lineTo(0f, y0 + r)
            p.arcTo(Rect(0f, y0, 2 * r, y0 + 2 * r), 180f, 90f, false)
            p.lineTo(size.width - r, y0)
            p.arcTo(Rect(size.width - 2 * r, y0, size.width, y0 + 2 * r), 270f, 90f, false)
        } else {
            p.lineTo(0f, y0)
            p.moveTo(size.width, y0)
        }
        p.lineTo(size.width, if (bottom) y1 - r else y1)
        if (bottom) {
            p.arcTo(Rect(size.width - 2 * r, y1 - 2 * r, size.width, y1), 0f, 90f, false)
            p.lineTo(r, y1)
            p.arcTo(Rect(0f, y1 - 2 * r, 2 * r, y1), 90f, 90f, false)
        }
        drawPath(p, stroke, style = Stroke(width = w))
        if (!top) drawRect(sep, Offset(WorkTextColumn.toPx(), -bridge - w / 2), Size(size.width - WorkTextColumn.toPx(), w))
    }
}

/** A step's content inset: from the card's edge, or (timeline) from the words' column right of the line. */
@Composable
private fun Modifier.stepInset(): Modifier =
    if (workTimeline) padding(start = WorkTextColumn, end = JSpace.xs, top = JSpace.m, bottom = JSpace.m)
    else padding(horizontal = WorkInset, vertical = JSpace.m)

/** One step of an opened work line (iOS `WorkStepView`). */
@Composable
fun WorkStepRow(step: FoldedRow.Step, working: Boolean) {
    Box(Modifier.fillMaxWidth().workSlice(top = step.first, bottom = false)) {
        when (val w = step.item) {
            is WorkItem.Call -> ToolCallRow(w.call, working, grouped = true)
            is WorkItem.Thought -> Box(Modifier.stepInset()) {
                if (w.item.kind == "notice") NoticeRow(w.item, inWork = true) else ThinkingRow(w.item)
            }
            is WorkItem.Text -> AssistantText(w.item, Modifier.stepInset(), quiet = workTimeline)
            is WorkItem.Image -> {
                val env = LocalChatEnv.current
                if (env != null) {
                    val img = remember(w.item.id) { imageOf(env.key, w.item) }
                    ChatThumb(img, modifier = Modifier.stepInset(), width = w.item.image?.width, height = w.item.image?.height)
                }
            }
        }
    }
}

/** The card's foot: 「收起 N 步」, so a long list needs no scroll back to its head. */
@Composable
fun WorkEndRow(count: Int, onCollapse: () -> Unit) {
    val c = chatColors
    Box(Modifier.fillMaxWidth().workSlice(top = false, bottom = true).clickable(onClick = onCollapse).stepInset()) {
        Text("收起 $count 步", style = JTheme.type.captionMedium, color = c.accent, maxLines = 1)
    }
}

@Composable
fun WorkingIndicator(label: String) {
    val c = chatColors
    Row(Modifier.padding(vertical = JSpace.xs), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s)) {
        CircularProgressIndicator(Modifier.size(JSize.markS), strokeWidth = JSize.lineMedium, color = chatColors.working)
        Text(label, style = JTheme.type.caption, color = c.secondaryLabel, maxLines = 1)
    }
}

/** A picture row (outside a work line): the agent's on the left, at most 280 dp tall. */
@Composable
fun ImageRow(item: Item, key: SessionKey) {
    val img = remember(item.id) { imageOf(key, item) }
    ChatThumb(img, modifier = Modifier.fillMaxWidth(0.8f), width = item.image?.width, height = item.image?.height)
}
