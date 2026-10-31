package jarvis.android.ui.chat

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
import kotlinx.coroutines.launch

// The chat's message rows (ios/Jarvis/Views/Chat/MessageViews.swift). Tools are in chat/ToolViews.kt.

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
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(6.dp)) {
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
                .background(c.userBubble.copy(alpha = if (faded) 0.6f else 1f), RoundedCornerShape(18.dp))
                .padding(horizontal = 14.dp, vertical = 10.dp),
        )
    }
}

/** A message another Claude session sent this one: on the left in a quiet card naming the sender. */
@Composable
private fun PeerMessage(item: Item, text: String, from: String) {
    val c = chatColors
    Column(
        Modifier.padding(end = 32.dp).fillMaxWidth().clip(RoundedCornerShape(12.dp)).background(c.fillWell)
            .border(0.5.dp, c.label.copy(alpha = 0.08f), RoundedCornerShape(12.dp))
            .messageLongPress(item.id, text, isUser = false).padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            Icon(Icons.Rounded.SwapHoriz, null, Modifier.size(14.dp), tint = c.secondaryLabel)
            Text("来自 $from", fontSize = 12.sp, fontWeight = FontWeight.SemiBold, color = c.secondaryLabel, maxLines = 1)
        }
        Text(text, fontSize = 16.sp, lineHeight = 22.sp, color = c.label)
    }
}

/** `<jarvis-context>` excerpts a message carries: quote cards; a tap opens the page they came from. */
@Composable
fun ContextCards(refs: List<ContextRef>) {
    val c = chatColors
    val from = jarvis.android.ui.web.LocalLinkSource.current // the page opens in the link sheet, not a browser
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        refs.forEach { r ->
            Row(
                Modifier.widthIn(max = 300.dp).clip(RoundedCornerShape(12.dp)).background(c.fillWell)
                    .clickable(enabled = r.url.startsWith("http")) { jarvis.android.ui.web.Links.open(r.url, from) }.height(IntrinsicSize.Min),
            ) {
                Box(Modifier.padding(vertical = 8.dp).width(3.dp).fillMaxHeight().background(c.accent.copy(alpha = 0.5f), CircleShape))
                Column(Modifier.padding(10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                        Icon(if (r.kind == "edit") Icons.Rounded.AutoAwesome else Icons.Rounded.FormatQuote, null, Modifier.size(13.dp), tint = c.accent)
                        Text((if (r.kind == "edit") "要改的内容 · " else "上下文 · ") + r.title, fontSize = 12.sp, fontWeight = FontWeight.SemiBold,
                            color = c.accent, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    Text(r.text, fontSize = 13.sp, lineHeight = 18.sp, color = c.secondaryLabel, maxLines = 3, overflow = TextOverflow.Ellipsis)
                    val host = remember(r.url) { runCatching { java.net.URI(r.url).host }.getOrNull().orEmpty() }
                    if (host.isNotEmpty()) Text(host, fontSize = 11.sp, color = c.tertiaryLabel, maxLines = 1)
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
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp, Alignment.End)) {
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
                Box(Modifier.size(side).clip(RoundedCornerShape(8.dp)).background(chatColors.fillChip), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
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
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(6.dp, Alignment.End)) {
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
        horizontalArrangement = Arrangement.spacedBy(6.dp, if (alignEnd) Alignment.End else Alignment.Start),
    ) {
        paths.forEach { p ->
            Row(
                Modifier.clip(CircleShape).background(c.accent.copy(alpha = 0.1f)).clickable { env.openFile(p) }.padding(horizontal = 9.dp, vertical = 5.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                Icon(fileIcon(p), null, Modifier.size(12.dp), tint = c.accent)
                Text(p.trimEnd('/').substringAfterLast('/').ifEmpty { p }, fontSize = 12.sp, fontWeight = FontWeight.Medium, color = c.accent, maxLines = 1)
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
    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.End, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        if (parsed.first.isNotEmpty()) ContextCards(parsed.first)
        if (p.images.isNotEmpty()) {
            val side = if (p.images.size == 1) 120.dp else 76.dp
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                p.images.forEach { SquareThumb(remember(it) { ViewerImage(data = it.data, mime = it.mime) }, side, null) }
            }
        }
        Row(verticalAlignment = Alignment.Bottom, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            when {
                bad -> Box {
                    Icon(
                        Icons.Rounded.Error, contentDescription = "发送失败，重发或删除", tint = c.danger,
                        modifier = Modifier.padding(bottom = 10.dp).size(22.dp).clip(CircleShape).clickable { menu = true },
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
                    Icon(Icons.Rounded.Schedule, contentDescription = "发送中", tint = c.tertiaryLabel, modifier = Modifier.padding(bottom = 12.dp).size(13.dp))
                // Written offline: a clock that offers 现在重试 / 不发了 (iOS Outbox).
                p.phase == PendingMessage.Phase.WAITING -> Box {
                    Icon(
                        Icons.Rounded.Schedule, contentDescription = "连上后自动发送，点按可以现在重试或不发了", tint = c.secondaryLabel,
                        modifier = Modifier.padding(bottom = 10.dp).size(22.dp).clip(CircleShape).clickable { menu = true }.padding(3.dp),
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
            Text("连上后自动发送", fontSize = 12.sp, color = c.secondaryLabel, textAlign = TextAlign.End, maxLines = 1)
        }
        if (bad) {
            Text(
                when (p.phase) {
                    // The hub's own error text is English; say it in the app's words.
                    PendingMessage.Phase.REFUSED -> if (p.fields["code"] == "dialog_open") "电脑上有对话框，没有发送" else "电脑上的输入框里有内容，没有发送"
                    PendingMessage.Phase.UNKNOWN -> "可能已经送到电脑上了：先看看对话里有没有，再决定要不要重发"
                    else -> p.error ?: "没发出去"
                },
                fontSize = 12.sp,
                color = c.danger,
                textAlign = TextAlign.End,
                maxLines = 3,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.padding(start = 48.dp),
            )
        }
    }
}

// --- Assistant -------------------------------------------------------------------------------------------------------

/** The agent's words: Markdown, no bubble, reading first; file chips; 看全文 when the hub cut it. */
@Composable
fun AssistantText(item: Item, modifier: Modifier = Modifier) {
    FullTextLoader(item) { text ->
        Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Markdown(text, Modifier.fillMaxWidth().messageLongPress(item.id, text, isUser = false), selectable = false)
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
    Column(modifier.fillMaxWidth().animateContentSize(), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(6.dp)).clickable { open = !open }.padding(vertical = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(Icons.Rounded.Psychology, null, Modifier.size(14.dp), tint = c.secondaryLabel)
            Text(if (open) "思考过程" else ToolText.firstLine(text), fontSize = 12.sp, color = c.secondaryLabel, maxLines = 1,
                overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            Icon(Icons.AutoMirrored.Rounded.KeyboardArrowRight, null, Modifier.size(14.dp).rotate(if (open) 90f else 0f), tint = c.secondaryLabel)
        }
        if (open) {
            Row(Modifier.height(IntrinsicSize.Min)) {
                Box(Modifier.width(2.dp).fillMaxHeight().background(c.secondaryLabel.copy(alpha = 0.3f)))
                SelectionContainer {
                    Text(text, fontSize = 13.sp, lineHeight = 18.sp, color = c.secondaryLabel, modifier = Modifier.padding(start = 10.dp))
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
                Modifier.clip(CircleShape).background(c.fillWell).padding(horizontal = 12.dp, vertical = 6.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Icon(if (failed) Icons.Rounded.Error else Icons.Rounded.Notifications, null, Modifier.size(13.dp), tint = if (failed) c.danger else c.accent)
                Text(summary, fontSize = 12.sp, color = c.secondaryLabel, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
        } else {
            Text(
                text, fontSize = 12.sp, color = c.secondaryLabel, textAlign = TextAlign.Center, maxLines = 4, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.clip(RoundedCornerShape(14.dp)).background(c.fillWell).padding(horizontal = 12.dp, vertical = 5.dp),
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
            Modifier.clip(CircleShape).background(c.fillWell).clickable { open = true }.padding(horizontal = 10.dp, vertical = 5.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Icon(Icons.Rounded.History, null, Modifier.size(15.dp), tint = c.accent)
            Text("离开期间回顾" + (item.time?.let { " · " + relativeTime(it, now) } ?: ""), fontSize = 13.sp, color = c.secondaryLabel, maxLines = 1)
            Icon(Icons.AutoMirrored.Rounded.KeyboardArrowRight, null, Modifier.size(14.dp), tint = c.secondaryLabel)
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
        Column(Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).navigationBarsPadding().padding(bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp)) {
            Text("离开期间回顾", fontSize = 17.sp, fontWeight = FontWeight.SemiBold, color = c.label, modifier = Modifier.align(Alignment.CenterHorizontally))
            item.time?.let { Text("写于 " + relativeTime(it, now), fontSize = 13.sp, color = c.secondaryLabel) }
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
            Modifier.weight(1f, fill = false).clip(RoundedCornerShape(6.dp)).clickable(enabled = w.items.isNotEmpty(), onClick = onToggle)
                .padding(vertical = 4.dp, horizontal = 2.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            if (w.running) CircularProgressIndicator(Modifier.size(12.dp), strokeWidth = 1.5.dp, color = chatColors.working)
            BoxWithConstraints(Modifier.weight(1f, fill = false)) {
                val full = workLabel(w.stats, w.running, now)
                val measurer = androidx.compose.ui.text.rememberTextMeasurer()
                val width = remember(full, measurer) {
                    measurer.measure(full, androidx.compose.ui.text.TextStyle(fontSize = 13.sp), softWrap = false, maxLines = 1).size.width
                }
                val tooLong = width > constraints.maxWidth
                Text(if (tooLong) workLabel(w.stats, w.running, now, short = true) else full, fontSize = 13.sp, color = c.secondaryLabel,
                    maxLines = 1, softWrap = false)
            }
            if (w.items.isNotEmpty()) {
                Icon(Icons.AutoMirrored.Rounded.KeyboardArrowRight, if (w.expanded) "收起过程" else "展开过程",
                    Modifier.size(14.dp).rotate(if (w.expanded) 90f else 0f), tint = c.secondaryLabel)
            }
        }
        if (onTerminal != null) {
            Spacer(Modifier.width(8.dp))
            Icon(Icons.Rounded.Terminal, contentDescription = "打开终端", tint = c.secondaryLabel,
                modifier = Modifier.size(32.dp).clip(CircleShape).clickable(onClick = onTerminal).padding(8.dp))
        }
    }
}

/** Half the chat list's spacing: a slice reaches that far into the gap to meet its neighbours. */
val WorkBridge: Dp = 6.dp

/**
 * A slice of an open work line's card (iOS `WorkCardSlice`): each step is a lazy list line of its own, so each draws
 * its part — rounded on top for the first, at the bottom for the 收起 foot, bridging the list's gaps; a hairline from
 * the words' column between steps.
 */
@Composable
fun Modifier.workSlice(top: Boolean, bottom: Boolean): Modifier {
    val c = chatColors
    val fill = c.fillWell
    val stroke = c.cardStroke
    val sep = c.separator
    return this.drawBehind {
        val bridge = WorkBridge.toPx()
        val r = 12.dp.toPx()
        val y0 = if (top) 0f else -bridge
        val y1 = if (bottom) size.height else size.height + bridge
        val rect = Rect(0f, y0, size.width, y1)
        val cr = CornerRadius(r)
        val rr = RoundRect(rect, topLeft = if (top) cr else CornerRadius.Zero, topRight = if (top) cr else CornerRadius.Zero,
            bottomLeft = if (bottom) cr else CornerRadius.Zero, bottomRight = if (bottom) cr else CornerRadius.Zero)
        drawPath(Path().apply { addRoundRect(rr) }, fill)
        // The outline: the sides, plus the rounded top / bottom where the card starts / ends.
        val w = 0.5.dp.toPx()
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

/** One step of an opened work line (iOS `WorkStepView`). */
@Composable
fun WorkStepRow(step: FoldedRow.Step, working: Boolean) {
    Box(Modifier.fillMaxWidth().workSlice(top = step.first, bottom = false)) {
        when (val w = step.item) {
            is WorkItem.Call -> ToolCallRow(w.call, working, grouped = true)
            is WorkItem.Thought -> Box(Modifier.padding(horizontal = WorkInset, vertical = 10.dp)) {
                if (w.item.kind == "notice") NoticeRow(w.item, inWork = true) else ThinkingRow(w.item)
            }
            is WorkItem.Text -> AssistantText(w.item, Modifier.padding(horizontal = WorkInset, vertical = 10.dp))
            is WorkItem.Image -> {
                val env = LocalChatEnv.current
                if (env != null) {
                    val img = remember(w.item.id) { imageOf(env.key, w.item) }
                    ChatThumb(img, modifier = Modifier.padding(horizontal = WorkInset, vertical = 10.dp), width = w.item.image?.width, height = w.item.image?.height)
                }
            }
        }
    }
}

/** The card's foot: 「收起 N 步」, so a long list needs no scroll back to its head. */
@Composable
fun WorkEndRow(count: Int, onCollapse: () -> Unit) {
    val c = chatColors
    Box(Modifier.fillMaxWidth().workSlice(top = false, bottom = true).clickable(onClick = onCollapse).padding(horizontal = WorkInset, vertical = 10.dp)) {
        Text("收起 $count 步", fontSize = 13.sp, fontWeight = FontWeight.Medium, color = c.accent, maxLines = 1)
    }
}

@Composable
fun WorkingIndicator(label: String) {
    val c = chatColors
    Row(Modifier.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        CircularProgressIndicator(Modifier.size(14.dp), strokeWidth = 1.8.dp, color = chatColors.working)
        Text(label, fontSize = 13.sp, color = c.secondaryLabel, maxLines = 1)
    }
}

/** A picture row (outside a work line): the agent's on the left, at most 280 dp tall. */
@Composable
fun ImageRow(item: Item, key: SessionKey) {
    val img = remember(item.id) { imageOf(key, item) }
    ChatThumb(img, modifier = Modifier.fillMaxWidth(0.8f), width = item.image?.width, height = item.image?.height)
}
