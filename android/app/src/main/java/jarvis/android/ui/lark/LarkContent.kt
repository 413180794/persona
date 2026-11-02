package jarvis.android.ui.lark

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import jarvis.android.lark.LarkFormat
import jarvis.android.lark.LarkInline
import jarvis.android.lark.LarkMarkdown
import jarvis.android.lark.LarkPart
import jarvis.android.lark.LarkPartRuns
import jarvis.android.lark.LarkParts
import jarvis.android.lark.LarkRichBlock
import jarvis.android.lark.LarkRichNode
import jarvis.android.lark.LarkSyntax
import jarvis.android.lark.LarkTextBase
import jarvis.android.lark.LarkTint
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.LarkChrome
import jarvis.android.ui.design.LarkFileKinds
import jarvis.android.ui.design.LarkIcons
import jarvis.android.ui.design.LarkSize
import jarvis.android.ui.design.LarkType
import jarvis.android.ui.media.MediaShare

// The body of one Feishu message (iOS LarkMessageContent.swift): rich text, pictures, stickers, files, video, voice,
// code and cards.

/** Parts of a message; rich-text leaves with block ancestors are rebuilt into lark-connector's block tree. */
@Composable
fun LarkPartsView(parts: List<LarkPart>, modifier: Modifier = Modifier, base: LarkTextBase = LarkTextBase()) {
    val structured = remember(parts) { parts.any { it.blocks.isNotEmpty() } }
    val nodes = remember(parts) { if (structured) LarkRichNode.tree(parts) else parts.map { LarkRichNode.Leaf(it) } }
    LarkNodes(nodes, structured, base, modifier)
}

private sealed interface Chunk {
    data class Inline(val runs: List<LarkInline>) : Chunk
    data class Node(val node: LarkRichNode) : Chunk
}

private fun chunks(nodes: List<LarkRichNode>, structured: Boolean): List<Chunk> {
    val out = ArrayList<Chunk>()
    val leaves = ArrayList<LarkPart>()
    fun flush() {
        if (leaves.isEmpty()) return
        val runs = LarkPartRuns.flowRuns(leaves, structured)
        if (runs.isNotEmpty()) out += Chunk.Inline(runs)
        leaves.clear()
    }
    for (n in nodes) {
        if (n is LarkRichNode.Leaf && LarkPartRuns.isInline(n.part)) leaves += n.part
        else { flush(); out += Chunk.Node(n) }
    }
    flush()
    return out
}

/** Consecutive inline leaves become one wrapped text; everything else stacks. */
@Composable
fun LarkNodes(nodes: List<LarkRichNode>, structured: Boolean, base: LarkTextBase, modifier: Modifier = Modifier) {
    val list = remember(nodes, structured) { chunks(nodes, structured) }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        for (c in list) when (c) {
            is Chunk.Inline -> LarkInlineText(c.runs, base = base)
            is Chunk.Node -> when (val n = c.node) {
                is LarkRichNode.Leaf -> LarkBlockPart(n.part)
                is LarkRichNode.Block -> LarkBlockView(n.block, n.children, base)
            }
        }
    }
}

@Composable
private fun LarkBlockView(b: LarkRichBlock, children: List<LarkRichNode>, base: LarkTextBase) {
    val c = JTheme.colors
    when {
        b.type == "ordered-list" || b.type == "unordered-list" -> Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            children.forEachIndexed { i, item ->
                Row(horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    Text(LarkRichNode.marker(b, i), style = LarkType.marker(base.size, c.secondaryText, TextAlign.End), modifier = Modifier.widthIn(min = JSize.markS))
                    Box(Modifier.weight(1f)) { LarkNodes(listOf(item), true, base) }
                }
            }
        }
        b.type == "quote" -> Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            Box(Modifier.width(JSize.bar).fillMaxHeight().clip(CircleShape).background(c.label.copy(alpha = 0.18f)))
            LarkNodes(children, true, base.copy(color = LarkTint.secondary))
        }
        b.type.startsWith("heading-") -> {
            val size = when (b.type.removePrefix("heading-").toIntOrNull() ?: 3) { 1 -> 22f; 2 -> 20f; 3 -> 18f; 4 -> 17f; else -> 16f }
            LarkNodes(children, true, LarkTextBase(size = size, bold = true))
        }
        else -> LarkNodes(children, true, base)
    }
}

/** A part that is not inline text: picture, sticker, file, video / voice, code, card, removed card, Markdown, system line. */
@Composable
fun LarkBlockPart(part: LarkPart) {
    when (part.kind) {
        "image" -> LarkImagePart(part)
        "emotion" -> LarkSticker(part)
        "file" -> LarkFileCard(part)
        "media" -> if ((part.metaString("mime") ?: "").startsWith("audio/")) LarkVoicePart(part) else LarkVideoPart(part)
        "code" -> LarkCodeBlock(part.text ?: "", part.metaString("languageName") ?: part.metaString("language"))
        "card" -> LarkCardView(part)
        "link" -> LarkMomentsCard(part)
        "interactive" -> HorizontalDivider(color = JTheme.colors.separator)
        "text" -> if (part.metaBool("system")) LarkSystemText(part) else LarkMarkdownView(part.text ?: "")
        else -> Text(part.text ?: "[${part.kind}]", style = JTheme.type.secondary, color = JTheme.colors.secondaryText)
    }
}

/** Group events ("韩梅梅 把群名改成了…"): the people named in the template stand out. */
@Composable
fun LarkSystemText(part: LarkPart, modifier: Modifier = Modifier) {
    val runs = remember(part) { LarkPartRuns.systemRuns(part) }
    LarkInlineText(runs, modifier, base = LarkTextBase(size = 13f, color = LarkTint.secondary), align = TextAlign.Center)
}

/** A Markdown text part (bots): headings, lists, quotes and inline marks. */
@Composable
fun LarkMarkdownView(text: String, base: LarkTextBase = LarkTextBase()) {
    val lines = remember(text) { LarkMarkdown.lines(text) }
    val c = JTheme.colors
    SelectionContainer {
        Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            for (l in lines) {
                if (l.runs.isEmpty()) { Spacer(Modifier.height(JSpace.xxs)); continue }
                val b = when (l.heading) {
                    0 -> base.copy(color = if (l.quote) LarkTint.secondary else base.color)
                    1 -> base.copy(size = 20f, bold = true)
                    2 -> base.copy(size = 18f, bold = true)
                    else -> base.copy(size = 17f, bold = true)
                }
                Row(Modifier.padding(start = JSpace.l * l.indent), horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    if (l.quote) Box(Modifier.width(JSize.bar).height((b.size * LarkType.LINE).dp).background(c.label.copy(alpha = 0.18f)))
                    if (l.marker != null) Text(l.marker, style = LarkType.marker(b.size, c.secondaryText))
                    LarkInlineText(l.runs, base = b)
                }
            }
        }
    }
}

private fun kotlinx.serialization.json.JsonElement?.dblOr0(): Double =
    (this as? kotlinx.serialization.json.JsonPrimitive)?.content?.toDoubleOrNull() ?: 0.0

/** A picture at its own aspect ratio inside 240 × 300; a tap opens the viewer on every picture of the message. */
@Composable
fun LarkImagePart(part: LarkPart) {
    val url = part.url
    val ctx = LocalLarkContext.current
    val actions = LocalLarkActions.current
    if (url == null) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            Icon(LarkIcons.expired, null, Modifier.size(JSize.badge), tint = JTheme.colors.secondaryText)
            Text("图片已失效", style = JTheme.type.secondary, color = JTheme.colors.secondaryText)
        }
        return
    }
    val (w, h) = LarkParts.displaySize(part.meta("width").dblOr0(), part.meta("height").dblOr0(), LarkSize.PICTURE_W, LarkSize.PICTURE_H)
    val shape = RoundedCornerShape(JRadius.control)
    val px = with(LocalDensity.current) { maxOf(w, h).dp.roundToPx() }
    LarkRemoteImage(
        url,
        Modifier.size(w.dp, h.dp).clip(shape).border(JSize.hairline, JTheme.colors.label.copy(alpha = 0.06f), shape)
            .clickable { val all = ctx.images.ifEmpty { listOf(url) }; actions.openImages(all, all.indexOf(url).coerceAtLeast(0)) }
            .semantics { contentDescription = "图片" },
        maxPx = px.coerceIn(PICTURE_MIN_PX, PICTURE_MAX_PX),
    )
}

private const val PICTURE_MIN_PX = 300
private const val PICTURE_MAX_PX = 1200

/** A sticker at its own aspect ratio inside its box (no blank band under a wide one). */
@Composable
fun LarkSticker(part: LarkPart) {
    val url = part.url ?: return
    val hub = LocalLarkHub.current
    var bmp by remember(url) { mutableStateOf(hub?.let { LarkMedia.cached(it.api, url, null, STICKER_PX) }) }
    LaunchedEffect(url, hub) { if (bmp == null && hub != null) bmp = LarkMedia.image(hub.api, url, maxPx = STICKER_PX) }
    val b = bmp
    if (b == null) {
        Box(Modifier.size(JSize.box80).clip(RoundedCornerShape(JRadius.control)).background(JTheme.colors.label.copy(alpha = 0.05f)))
    } else {
        val r = minOf(LarkSize.STICKER / maxOf(b.width, 1), LarkSize.STICKER / maxOf(b.height, 1))
        androidx.compose.foundation.Image(b.asImageBitmap(), part.text ?: "贴纸", Modifier.size((b.width * r).dp, (b.height * r).dp), contentScale = ContentScale.Fit)
    }
}

private const val STICKER_PX = 400

/** A file: its name and size, a type tile; a tap downloads it and opens it in another app. */
@Composable
fun LarkFileCard(part: LarkPart) {
    val actions = LocalLarkActions.current
    val name = part.text ?: "文件"
    val ext = (part.metaString("extension") ?: name.substringAfterLast('.', "")).uppercase()
    val enabled = part.url != null && !part.metaBool("destroyed")
    val shape = RoundedCornerShape(JRadius.card)
    Row(
        Modifier.width(LarkSize.fileCard).clip(shape).background(LarkChrome.cardBackground).border(JSize.hairline, LarkChrome.cardStroke, shape)
            .clickable(enabled = enabled) { part.url?.let { actions.openFile(it, name) } }.padding(JSpace.m),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
            Text(name, style = JTheme.type.secondaryMedium, color = JTheme.colors.label, maxLines = 2, overflow = TextOverflow.Ellipsis)
            Text(LarkFormat.fileSize(part.metaString("size")), style = JTheme.type.label, color = JTheme.colors.secondaryText)
        }
        FileTile(ext)
    }
}

@Composable
private fun FileTile(ext: String) {
    val (icon, color) = LarkFileKinds.of(ext)
    Box(
        Modifier.size(JSize.avatarM).clip(RoundedCornerShape(JRadius.control)).background(Brush.verticalGradient(listOf(color.copy(alpha = 0.85f), color))),
        contentAlignment = Alignment.Center,
    ) { Icon(icon, ext, Modifier.size(JSize.iconS), tint = Color.White) }
}

/** A video: its poster with a play button and the duration; a tap plays it full screen. */
@Composable
fun LarkVideoPart(part: LarkPart) {
    val actions = LocalLarkActions.current
    val (w, h) = LarkParts.displaySize(part.meta("posterWidth").dblOr0(), part.meta("posterHeight").dblOr0(), LarkSize.PICTURE_W, LarkSize.PICTURE_H)
    Box(
        Modifier.size(w.dp, h.dp).clip(RoundedCornerShape(JRadius.control)).background(Color.Black.copy(alpha = 0.85f))
            .clickable(enabled = part.url != null) { part.url?.let { actions.playMedia(it, false, part.text ?: "视频") } }
            .semantics { contentDescription = "视频 ${part.text ?: ""}" },
        contentAlignment = Alignment.Center,
    ) {
        part.metaString("posterUrl")?.let { LarkRemoteImage(it, Modifier.matchParentSize(), maxPx = POSTER_PX) }
        Box(Modifier.size(JSize.avatarXL).clip(CircleShape).background(Color.Black.copy(alpha = 0.45f)), contentAlignment = Alignment.Center) {
            Icon(LarkIcons.play, "播放", Modifier.size(JSize.wellXS), tint = Color.White)
        }
        val d = LarkFormat.duration(part.meta("duration").dblOr0())
        if (d.isNotEmpty()) {
            Text(d, style = JTheme.type.labelStrong, color = Color.White,
                modifier = Modifier.align(Alignment.BottomEnd).padding(JSpace.sm).clip(CircleShape).background(Color.Black.copy(alpha = 0.5f))
                    .padding(horizontal = JSpace.sm, vertical = JSpace.xxs))
        }
    }
}

private const val POSTER_PX = 900

/** A voice message: the play glyph, a waveform, the duration. */
@Composable
fun LarkVoicePart(part: LarkPart) {
    val actions = LocalLarkActions.current
    val ms = part.meta("duration").dblOr0()
    val c = JTheme.colors
    Row(
        Modifier.clip(RoundedCornerShape(JRadius.control)).clickable(enabled = part.url != null) { part.url?.let { actions.playMedia(it, true, "语音") } }
            .padding(vertical = JSpace.xxs),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.s),
    ) {
        Icon(LarkIcons.playVoice, "播放语音", Modifier.size(JSize.iconL), tint = LarkChrome.blue)
        Row(horizontalArrangement = Arrangement.spacedBy(JSpace.xxs), verticalAlignment = Alignment.CenterVertically) {
            val heights = listOf(JSize.dot, JSize.markS, JSize.dotXL, JSize.badge, JSize.markXS, JSize.dotXS)
            repeat((ms / VOICE_MS_PER_BAR).toInt().coerceIn(VOICE_BARS_MIN, VOICE_BARS_MAX)) { i ->
                Box(Modifier.width(JSize.rule).height(heights[i % heights.size]).clip(CircleShape).background(c.label.copy(alpha = 0.35f)))
            }
        }
        Text(LarkFormat.duration(ms), style = JTheme.type.secondary, color = c.secondaryText)
    }
}

private const val VOICE_MS_PER_BAR = 1500
private const val VOICE_BARS_MIN = 5
private const val VOICE_BARS_MAX = 18

/** A code block: the language, a copy button, light syntax colors, scrolled sideways. */
@Composable
fun LarkCodeBlock(code: String, language: String?) {
    val c = JTheme.colors
    val ctx = LocalContext.current
    val (comment, string, number, keyword) = LarkChrome.syntax()
    val text = remember(code, comment) {
        buildAnnotatedString {
            for (t in LarkSyntax.tokens(code)) {
                val color = when (t.kind) {
                    LarkSyntax.Kind.COMMENT -> comment
                    LarkSyntax.Kind.STRING -> string
                    LarkSyntax.Kind.NUMBER -> number
                    LarkSyntax.Kind.KEYWORD -> keyword
                    LarkSyntax.Kind.PLAIN -> null
                }
                if (color == null) append(t.text) else withStyle(SpanStyle(color = color)) { append(t.text) }
            }
        }
    }
    Column(Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.control)).background(c.codeBackground)) {
        Row(Modifier.fillMaxWidth().padding(start = JSpace.m, end = JSpace.xs, top = JSpace.xs), verticalAlignment = Alignment.CenterVertically) {
            Text(language?.takeIf { it.isNotEmpty() } ?: "代码", style = JTheme.type.labelMedium, color = c.secondaryText, modifier = Modifier.weight(1f))
            Box(
                Modifier.size(JSize.wellL).clip(CircleShape).clickable {
                    MediaShare.copyText(ctx, code, "代码")
                    Toasts.show("已拷贝代码", ToastStyle.SUCCESS)
                }.semantics { contentDescription = "拷贝代码" },
                contentAlignment = Alignment.Center,
            ) { Icon(JIcons.copy, null, Modifier.size(JSize.markM), tint = c.secondaryText) }
        }
        SelectionContainer {
            Text(text, Modifier.horizontalScroll(rememberScrollState()).padding(start = JSpace.m, end = JSpace.m, bottom = JSpace.m),
                style = LarkType.code(c.label), softWrap = false)
        }
    }
}

/** Gives [content] the message's render context (its id and its pictures). */
@Composable
fun WithLarkMessage(messageId: String?, parts: List<LarkPart>, content: @Composable () -> Unit) {
    val ctx = remember(messageId, parts) { LarkRenderContext(messageId, LarkParts.imageUrls(parts)) }
    CompositionLocalProvider(LocalLarkContext provides ctx, content = content)
}
