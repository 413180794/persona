package jarvis.android.ui.lark

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.clipToBounds
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import jarvis.android.lark.LarkCardIcons
import jarvis.android.lark.LarkCardNode
import jarvis.android.lark.LarkCardRuns
import jarvis.android.lark.LarkCardStyle
import jarvis.android.lark.LarkFormat
import jarvis.android.lark.LarkPalette
import jarvis.android.lark.LarkPart
import jarvis.android.lark.LarkTextBase
import jarvis.android.lark.LarkTint
import jarvis.android.lark.arr
import jarvis.android.lark.bool
import jarvis.android.lark.dbl
import jarvis.android.lark.get
import jarvis.android.lark.int
import jarvis.android.lark.larkCardAction
import jarvis.android.lark.larkCardListMarkers
import jarvis.android.lark.str
import jarvis.android.ui.JTheme
import jarvis.android.ui.chat.ToastStyle
import jarvis.android.ui.chat.Toasts
import jarvis.android.ui.design.JIcons
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import jarvis.android.ui.design.LarkChrome
import jarvis.android.ui.design.LarkIcons
import jarvis.android.ui.design.LarkSize
import jarvis.android.ui.design.LarkType
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive

// Feishu interactive cards (iOS LarkCard.swift, a port of lark-connector's `card-renderer.tsx`): the header in the card's
// theme color, then the element tree — markdown, column_set, table, list, collapsible_panel, note, tags, icons,
// buttons (links / callbacks), read-only form controls, overflow, images, charts.

/** A card message (or one of lark-connector's summary cards when `metadata.variant` is set). */
@Composable
fun LarkCardView(part: LarkPart) {
    val meta = part.metadata ?: JsonObject(emptyMap())
    if (meta["variant"].str != null) {
        LarkSummaryCardView(meta, part.text ?: "")
        return
    }
    val theme = meta["theme"].str ?: "blue"
    val shape = RoundedCornerShape(JRadius.card)
    CompositionLocalProvider(LocalLarkTextBase provides LarkTextBase(size = 15f)) {
        Column(Modifier.fillMaxWidth().clip(shape).background(LarkChrome.cardBackground).border(JSize.hairline, LarkChrome.cardStroke, shape)) {
            val header = LarkCardNode.of(meta["header"])
            val title = meta["title"].str ?: part.text
            when {
                header != null -> LarkCardHeader(header, theme)
                meta["hideHeader"].bool != true && !title.isNullOrEmpty() -> LarkCardHeader(
                    LarkCardNode(JsonObject(mapOf("tag" to JsonPrimitive("card_header"),
                        "children" to JsonArray(listOf(JsonObject(mapOf("tag" to JsonPrimitive("plain_text"), "text" to JsonPrimitive(title)))))))),
                    theme,
                )
            }
            val body = LarkCardNode.of(meta["body"])
            if (body != null) {
                LarkCardFlow(body.children, Modifier.padding(JSpace.m), spacing = LarkCardStyle.spacing(body) ?: 10f)
            } else {
                val fallback = meta["fallback"].arr?.mapNotNull { it.str }.orEmpty()
                if (fallback.isNotEmpty()) Column(Modifier.padding(JSpace.m), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                    for (t in fallback) Text(t, style = JTheme.type.secondary, color = JTheme.colors.label)
                }
            }
        }
    }
}

/** The title row (leading icons, title, tags), an optional subtitle, tinted with the card's template color. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LarkCardHeader(node: LarkCardNode, theme: String) {
    val kids = node.children
    val texts = kids.filter { it.tag == "plain_text" }
    val tags = kids.filter { it.tag == "text_tag" }
    val extras = kids.filter { it.tag != "plain_text" && it.tag != "text_tag" }
    val tinted = !(theme.isEmpty() || theme == "default" || theme == "white")
    val hue = larkColor(LarkPalette.hue(theme))
    Column(
        Modifier.fillMaxWidth().background(if (tinted) hue.copy(alpha = 0.13f) else Color.Transparent).padding(horizontal = JSpace.m, vertical = JSpace.m),
        verticalArrangement = Arrangement.spacedBy(JSpace.xs),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            for (e in extras) LarkCardNodeView(e)
            texts.firstOrNull()?.let {
                LarkInlineText(remember(it) { LarkCardRuns.runs(it, inMarkdown = false) }, Modifier.weight(1f, fill = false), base = LarkTextBase(size = 17f, bold = true))
            }
        }
        if (tags.isNotEmpty()) FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            for (t in tags) LarkCardTag(t)
        }
        if (texts.size > 1) LarkInlineText(remember(texts[1]) { LarkCardRuns.runs(texts[1], inMarkdown = false) }, base = LarkTextBase(size = 13f, color = LarkTint.secondary))
    }
    if (!tinted) HorizontalDivider(color = LarkChrome.cardStroke, thickness = JSize.hairline)
}

private sealed interface CardChunk {
    data class Inline(val nodes: List<LarkCardNode>) : CardChunk
    data class Block(val node: LarkCardNode) : CardChunk
}

/** A list of card elements: consecutive inline elements wrap together as one paragraph; blocks stack. */
@Composable
fun LarkCardFlow(
    nodes: List<LarkCardNode>, modifier: Modifier = Modifier, inMarkdown: Boolean = false, spacing: Float = 6f,
    alignment: Alignment.Horizontal = Alignment.Start,
) {
    val chunks = remember(nodes) {
        val out = ArrayList<CardChunk>()
        val run = ArrayList<LarkCardNode>()
        for (n in nodes) {
            if (n.tag == "collapse_divider") continue
            if (n.isInline) run += n else {
                if (run.isNotEmpty()) { out += CardChunk.Inline(run.toList()); run.clear() }
                out += CardChunk.Block(n)
            }
        }
        if (run.isNotEmpty()) out += CardChunk.Inline(run.toList())
        out
    }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(spacing.dp), horizontalAlignment = alignment) {
        for (c in chunks) when (c) {
            is CardChunk.Inline -> LarkCardInlineRun(c.nodes, inMarkdown)
            is CardChunk.Block -> LarkCardNodeView(c.node)
        }
    }
}

/** Inline elements as text; a run of only tags and icons lays out as chips instead. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LarkCardInlineRun(nodes: List<LarkCardNode>, inMarkdown: Boolean) {
    val base = LocalLarkTextBase.current
    val chipsOnly = nodes.all { it.tag == "text_tag" || it.isIcon || (it.tag == "plain_text" && (it.text ?: "").isBlank()) }
    if (chipsOnly && nodes.any { it.tag == "text_tag" }) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            for (n in nodes) if (n.tag == "text_tag") LarkCardTag(n) else if (n.isIcon) LarkCardIcon(n, base.size.dp)
        }
        return
    }
    val align = nodes.firstOrNull()?.styleString("textAlign")
    val lines = if (nodes.size == 1) nodes[0].style("lines").int else null
    val runs = remember(nodes, inMarkdown) { nodes.flatMap { LarkCardRuns.runs(it, inMarkdown) } }
    LarkInlineText(
        runs, if (align == "center" || align == "right") Modifier.fillMaxWidth() else Modifier, base = base,
        maxLines = if ((lines ?: 0) > 0) lines!! else Int.MAX_VALUE,
        align = when (align) { "center" -> TextAlign.Center; "right" -> TextAlign.End; else -> TextAlign.Start },
    )
}

private fun alignmentOf(n: LarkCardNode): Alignment.Horizontal = when (n.styleString("textAlign")) {
    "center" -> Alignment.CenterHorizontally
    "right" -> Alignment.End
    else -> Alignment.Start
}

/** Margin, padding, background, border and corner radius from a card element's style (port of `cardStyle`). */
@Composable
private fun Modifier.cardBox(style: JsonElement?): Modifier {
    if (style == null) return this
    val pad = LarkCardStyle.insets(style["padding"])
    val margin = LarkCardStyle.insets(style["margin"])
    val bg = (LarkPalette.custom(style["customBackgroundStyle"]) ?: LarkPalette.background(style["backgroundStyle"].str))?.let { larkColor(it) }
    val radius = LarkCardStyle.unit(style["cornerRadiusValue"])?.dp ?: if (bg != null) JRadius.control else JRadius.badge
    val border = style["hasBorder"].bool == true
    val borderColor = LarkPalette.text(style["borderColor"].str)?.let { larkColor(it) } ?: LarkChrome.cardStroke
    val shape = RoundedCornerShape(radius)
    var m = this.padding(start = margin[1].dp, top = margin[0].dp, end = margin[3].dp, bottom = margin[2].dp)
    if (bg != null || border) m = m.clip(shape)
    if (bg != null) m = m.background(bg, shape)
    if (border) m = m.border(JSize.hairline, borderColor, RoundedCornerShape(maxOf(radius, JRadius.badge)))
    return m.padding(start = pad[1].dp, top = pad[0].dp, end = pad[3].dp, bottom = pad[2].dp)
}

/** One block-level card element (port of `CardNodeView`). */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LarkCardNodeView(node: LarkCardNode) {
    val n = node
    val base = LocalLarkTextBase.current
    val c = JTheme.colors
    Box(Modifier.cardBox(n.style)) {
        when (n.tag) {
            "body", "form" -> LarkCardFlow(n.children, spacing = LarkCardStyle.spacing(n) ?: 10f)
            "card_header" -> LarkCardHeader(n, "")
            "div" -> Column(verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                if (n.children.isNotEmpty()) LarkCardFlow(n.children, Modifier.fillMaxWidth(), alignment = alignmentOf(n))
                val fields = n.raw["fields"].arr.orEmpty()
                if (fields.isNotEmpty()) LarkCardFields(fields)
            }
            "markdown", "lark_md" -> {
                val kids = n.children
                val first = kids.firstOrNull()
                when {
                    first != null && first.isIcon -> Row(horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                        LarkCardIcon(first, base.size.dp)
                        LarkCardFlow(kids.drop(1), inMarkdown = true)
                    }
                    kids.isEmpty() && n.text != null -> LarkInlineText(remember(n) { LarkFormat.cardMarkdown(n.text!!) })
                    else -> LarkCardFlow(kids, if (alignmentOf(n) != Alignment.Start) Modifier.fillMaxWidth() else Modifier, inMarkdown = true, alignment = alignmentOf(n))
                }
            }
            "plain_text", "link", "at", "person", "person_v1", "code_span", "local_datetime" -> LarkCardInlineRun(listOf(n), false)
            "column_set" -> LarkCardColumns(n)
            "column" -> LarkCardColumn(n)
            "interactive_container" -> LarkCardContainer(n)
            "hr" -> HorizontalDivider(Modifier.padding(vertical = JSpace.xxs), color = LarkChrome.cardStroke, thickness = JSize.hairline)
            "img" -> LarkCardImage(n)
            "img_combination" -> {
                val imgs = n.children
                val cols = if (imgs.size >= 3) 3 else maxOf(imgs.size, 1)
                Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    for (row in imgs.chunked(cols)) Row(horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                        for (i in row) Box(Modifier.weight(1f)) { LarkCardImage(i, square = true) }
                        repeat(cols - row.size) { Spacer(Modifier.weight(1f)) }
                    }
                }
            }
            "action" -> FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
                for (ch in n.children) LarkCardNodeView(ch)
            }
            "button" -> LarkCardButton(n)
            "list" -> LarkCardList(n)
            "blockquote" -> Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
                Box(Modifier.width(JSize.bar).fillMaxHeight().clip(RoundedCornerShape(JSize.rule)).background(c.label.copy(alpha = 0.18f)))
                CompositionLocalProvider(LocalLarkTextBase provides base.copy(color = LarkTint.secondary)) { LarkCardFlow(n.children, inMarkdown = true) }
            }
            "code_block" -> LarkCodeBlock(n.text ?: n.plainText, n.raw["language"].str)
            "heading" -> CompositionLocalProvider(LocalLarkTextBase provides LarkTextBase(size = base.size + 2, bold = true)) {
                LarkCardFlow(n.children, inMarkdown = true)
            }
            "note" -> LarkCardNote(n)
            "text_tag" -> LarkCardTag(n)
            "standard_icon", "custom_icon", "ud_icon" -> LarkCardIcon(n, base.size.dp)
            "select_static", "multi_select_static", "select_person", "multi_select_person", "date_picker", "picker_datetime", "picker_time", "input" ->
                LarkCardField(n)
            "select_img" -> FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.sm), verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                for (o in n.raw["options"].arr.orEmpty()) {
                    val u = o["image"]["url"].str ?: continue
                    val shape = RoundedCornerShape(JRadius.control)
                    LarkRemoteImage(u, Modifier.size(JSize.box72).clip(shape).border(JSize.rule, if (o["selected"].bool == true) LarkChrome.blue else Color.Transparent, shape), maxPx = SMALL_PX)
                }
            }
            "checker" -> {
                val checked = n.raw["checked"].bool == true
                Row(horizontalArrangement = Arrangement.spacedBy(JSpace.s), verticalAlignment = Alignment.Top) {
                    Icon(if (checked) LarkIcons.checked else LarkIcons.unchecked, if (checked) "已完成" else "未完成",
                        Modifier.padding(top = JSpace.xxs).size(JSize.badge), tint = if (checked) LarkChrome.blue else c.secondaryText)
                    Box(Modifier.weight(1f).alpha(if (checked) 0.6f else 1f)) { LarkCardFlow(n.children) }
                }
            }
            "overflow" -> LarkCardOverflow(n)
            "collapsible_panel" -> LarkCardPanel(n)
            "table", "markdown_table" -> LarkCardTable(n)
            "markdown_table_row", "markdown_table_cell", "markdown_table_header" -> LarkCardFlow(n.children, inMarkdown = true)
            "chart" -> LarkCardChart(n.raw["chart"])
            "avatar" -> LarkAvatar(n.text ?: "?", n.imageUrl, size = JSize.icon)
            "audio", "markdown_audio", "video" -> Row(
                Modifier.fillMaxWidth().clip(RoundedCornerShape(JRadius.control)).background(c.label.copy(alpha = 0.05f)).padding(horizontal = JSpace.m, vertical = JSpace.s),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
            ) {
                Icon(LarkIcons.play, null, Modifier.size(JSize.badge), tint = c.label)
                Text(n.text ?: if (n.tag == "video") "视频" else "音频", style = JTheme.type.secondary, color = c.label)
            }
            "fallback_text" -> Text(n.plainText, style = JTheme.type.secondary, color = c.secondaryText)
            "collapse_divider" -> Unit
            else -> when {
                n.children.isNotEmpty() -> LarkCardFlow(n.children)
                !n.text.isNullOrEmpty() -> LarkInlineText(listOf(jarvis.android.lark.LarkInline.Text(n.text!!)))
            }
        }
    }
}

private const val SMALL_PX = 300

/** `column_set`: columns side by side by weight / fixed width / content width; narrow-screen modes re-flow them. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LarkCardColumns(node: LarkCardNode) {
    val cols = node.children.filter { it.tag == "column" }
    val spacing = LarkCardStyle.unit(node.style("horizontalSpacingValue")) ?: 8f
    val mode = node.styleString("flexMode") ?: "none"
    val density = LocalDensity.current
    val gap = with(density) { spacing.dp.roundToPx() }
    val fallback = with(density) { JSize.paneMinRight.roundToPx() }
    when (mode) {
        "bisect", "trisect", "stretch" -> {
            val perRow = when (mode) { "bisect" -> 2; "trisect" -> 3; else -> 1 }
            val minW = with(density) { LarkSize.gridColumnMin.toPx() }
            Layout(content = { for (c in cols) LarkCardNodeView(c) }, modifier = Modifier.fillMaxWidth()) { ms, cs ->
                val n = LarkCardStyle.gridColumns(if (cs.hasBoundedWidth) cs.maxWidth.toFloat() else Float.POSITIVE_INFINITY, perRow, minW, gap.toFloat())
                val w = if (cs.hasBoundedWidth) ((cs.maxWidth - gap * (n - 1)) / n).coerceAtLeast(0) else fallback
                val ps = ms.map { it.measure(Constraints(minWidth = w, maxWidth = w)) }
                val rows = ps.chunked(n)
                val heights = rows.map { r -> r.maxOf { it.height } }
                layout(if (cs.hasBoundedWidth) cs.maxWidth else w * n, heights.sum() + gap * maxOf(rows.size - 1, 0)) {
                    var y = 0
                    rows.forEachIndexed { ri, r ->
                        r.forEachIndexed { i, p -> p.place(i * (w + gap), y) }
                        y += heights[ri] + gap
                    }
                }
            }
        }
        "flow" -> FlowRow(horizontalArrangement = Arrangement.spacedBy(spacing.dp), verticalArrangement = Arrangement.spacedBy(spacing.dp)) {
            for (c in cols) LarkCardNodeView(c)
        }
        else -> {
            val specs = remember(cols) { cols.map { LarkCardStyle.columnSpec(it) } }
            val vertical = node.styleString("verticalAlign") ?: "top"
            Layout(content = { for (c in cols) LarkCardNodeView(c) }, modifier = Modifier.fillMaxWidth()) { ms, cs ->
                val total = if (cs.hasBoundedWidth) cs.maxWidth.toFloat() else fallback.toFloat()
                val pxSpecs = specs.map { s -> if (s is LarkCardStyle.ColumnSpec.Fixed) LarkCardStyle.ColumnSpec.Fixed(with(density) { s.dp.dp.toPx() }) else s }
                val widths = LarkCardStyle.columnWidths(pxSpecs.take(ms.size), total, gap.toFloat()) { i -> ms[i].maxIntrinsicWidth(Constraints.Infinity).toFloat() }
                val ps = ms.mapIndexed { i, m ->
                    val w = widths.getOrElse(i) { 0f }.toInt().coerceAtLeast(0)
                    m.measure(Constraints(minWidth = w, maxWidth = w))
                }
                val h = ps.maxOfOrNull { it.height } ?: 0
                layout(total.toInt(), h) {
                    var x = 0
                    ps.forEachIndexed { i, p ->
                        p.place(x, when (vertical) { "center", "middle" -> (h - p.height) / 2; "bottom" -> h - p.height; else -> 0 })
                        x += widths.getOrElse(i) { 0f }.toInt() + gap
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LarkCardColumn(node: LarkCardNode) {
    val spacing = LarkCardStyle.spacing(node) ?: 6f
    val padded = LarkPalette.background(node.styleString("backgroundStyle")) != null && node.style("padding") == null
    Box(Modifier.fillMaxWidth().then(if (padded) Modifier.padding(JSpace.s) else Modifier)) {
        if (node.styleString("direction") == "horizontal") {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(spacing.dp), verticalArrangement = Arrangement.spacedBy(spacing.dp)) {
                for (c in node.children) LarkCardNodeView(c)
            }
        } else LarkCardFlow(node.children, Modifier.fillMaxWidth(), spacing = spacing, alignment = alignmentOf(node))
    }
}

/** `interactive_container`: a framed group; a tap opens its link. */
@Composable
fun LarkCardContainer(node: LarkCardNode) {
    val actions = LocalLarkActions.current
    val link = LarkFormat.safeUrl(node.url) ?: LarkFormat.safeUrl(node.raw["behaviors"].arr?.firstOrNull()?.get("default_url").str)
    LarkCardFlow(node.children, Modifier.fillMaxWidth().then(if (link != null) Modifier.clickable { actions.openUrl(link) } else Modifier),
        spacing = LarkCardStyle.spacing(node) ?: 6f)
}

@Composable
fun LarkCardFields(fields: List<JsonElement>) {
    val rows = remember(fields) {
        LarkCardStyle.fieldRows(fields.map { (it["short"].bool ?: false) to it["children"].arr.orEmpty().mapNotNull { c -> LarkCardNode.of(c) } })
    }
    Column(verticalArrangement = Arrangement.spacedBy(JSpace.s)) {
        for (row in rows) Row(horizontalArrangement = Arrangement.spacedBy(JSpace.m)) { for (f in row) LarkCardFlow(f, Modifier.weight(1f)) }
    }
}

/** A card picture: the full width at its own aspect ratio (cropped past 320), small icons at half their size. */
@Composable
fun LarkCardImage(node: LarkCardNode, square: Boolean = false) {
    val url = node.imageUrl ?: return
    val actions = LocalLarkActions.current
    val w = node.imageWidth
    val h = node.imageHeight
    val shape = RoundedCornerShape(JRadius.control)
    val tap = Modifier.clickable(enabled = node.raw["preview"].bool != false) { actions.openImages(listOf(url), 0) }.semantics { contentDescription = node.text ?: "图片" }
    when {
        w > 0 && w <= SMALL_IMAGE && !square -> LarkRemoteImage(url, Modifier.size((w / 2).dp, (h / 2).dp).clip(shape).then(tap), maxPx = SMALL_PX, contentScale = ContentScale.Fit)
        square -> LarkRemoteImage(url, Modifier.fillMaxWidth().aspectRatio(1f).clip(shape).then(tap), maxPx = SQUARE_PX)
        else -> {
            val ratio = if (w > 0 && h > 0) (w / h).toFloat() else 16f / 9f
            LarkRemoteImage(url, Modifier.fillMaxWidth().heightIn(max = LarkSize.cardImageMax).aspectRatio(maxOf(ratio, 0.75f)).clip(shape).then(tap), maxPx = FULL_PX)
        }
    }
}

private const val SMALL_IMAGE = 96
private const val SQUARE_PX = 600
private const val FULL_PX = 1200

/** A button: a link opens in the app; a callback button presses the card action as the user. */
@Composable
fun LarkCardButton(node: LarkCardNode) {
    val actions = LocalLarkActions.current
    val ctx = LocalLarkContext.current
    val hub = LocalLarkHub.current
    val scope = rememberCoroutineScope()
    var pending by remember { mutableStateOf(false) }
    val kind = node.styleString("type") ?: "default"
    val link = LarkFormat.safeUrl(node.url)
    val callback = node.actionId != null && node.actionMethod == 2 && ctx.messageId != null && hub != null
    val enabled = !node.disabled && (link != null || callback)
    val fill = node.style("widthValue")["value"].str == "fill" || node.styleString("width") == "fill"
    val label = node.plainText.trim()
    val icon = node.children.firstOrNull { it.isIcon }
    val red = larkColor(LarkPalette.fixed("red"))
    val blue = LarkChrome.blue
    val fg = when (kind) {
        "primary", "primary_filled", "danger_filled" -> Color.White
        "danger", "danger_text" -> red
        "text", "primary_text", "laser" -> blue
        else -> JTheme.colors.label
    }
    val bg = when (kind) {
        "primary", "primary_filled" -> blue
        "danger_filled" -> red
        "text", "primary_text", "danger_text", "laser" -> Color.Transparent
        else -> LarkChrome.cardBackground
    }
    val bordered = kind !in setOf("primary", "primary_filled", "danger_filled", "text", "primary_text", "danger_text", "laser")
    val shape = RoundedCornerShape(JRadius.badge)
    Row(
        Modifier.then(if (fill) Modifier.fillMaxWidth() else Modifier).defaultMinSize(minHeight = JSize.well).clip(shape).background(bg)
            .then(if (bordered) Modifier.border(JSize.hairline, if (kind.startsWith("danger")) red.copy(alpha = 0.5f) else LarkChrome.cardStroke, shape) else Modifier)
            .clickable(enabled = enabled && !pending, role = Role.Button) {
                if (link != null) { actions.openUrl(link); return@clickable }
                val mid = ctx.messageId ?: return@clickable
                val aid = node.actionId ?: return@clickable
                val api = hub?.api ?: return@clickable
                pending = true
                scope.launch {
                    runCatching { api.larkCardAction(mid, aid) }
                        .onSuccess { Toasts.show("已提交「$label」", ToastStyle.SUCCESS); delay(REFRESH_AFTER_MS); actions.refresh?.invoke() }
                        .onFailure { Toasts.show("没提交成功：${it.message}", ToastStyle.ERROR) }
                    pending = false
                }
            }
            .alpha(if (enabled) 1f else 0.5f)
            .padding(horizontal = if (fill) JSpace.sm else JSpace.m, vertical = JSpace.sm)
            .semantics { contentDescription = label.ifEmpty { node.raw["tooltip"].str ?: "按钮" } },
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs, Alignment.CenterHorizontally),
    ) {
        if (pending) CircularProgressIndicator(Modifier.size(JSize.markXS), strokeWidth = JSize.lineMedium, color = fg)
        else if (icon != null) LarkCardIcon(icon, JSize.markS)
        if (label.isNotEmpty()) Text(label, style = JTheme.type.secondaryMedium, color = fg, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

/** After a card button, the card is read again once Feishu has had time to update it. */
private const val REFRESH_AFTER_MS = 1200L

@Composable
fun LarkCardList(node: LarkCardNode) {
    val base = LocalLarkTextBase.current
    val items = node.raw["items"].arr.orEmpty()
    val markers = remember(items) { larkCardListMarkers(items) }
    Column(verticalArrangement = Arrangement.spacedBy(JSpace.xs)) {
        items.forEachIndexed { i, item ->
            Row(Modifier.padding(start = JSpace.l * (item["level"].int ?: 0).coerceIn(0, 4)), horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
                Text(markers[i], style = LarkType.marker(base.size, JTheme.colors.secondaryText))
                LarkCardFlow(item["children"].arr.orEmpty().mapNotNull { LarkCardNode.of(it) }, Modifier.weight(1f), inMarkdown = true)
            }
        }
    }
}

@Composable
fun LarkCardNote(node: LarkCardNode) {
    CompositionLocalProvider(LocalLarkTextBase provides LarkTextBase(size = 12f, color = LarkTint.secondary)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm)) {
            for (k in node.children) {
                val u = k.imageUrl
                when {
                    k.tag == "img" && u != null -> LarkRemoteImage(u, Modifier.size(JSize.markM).clip(RoundedCornerShape(JSize.bar)), size = thumbEdge(JSize.markM), maxPx = NOTE_PX)
                    k.isIcon -> LarkCardIcon(k, JSize.markXS)
                    else -> LarkCardFlow(listOf(k))
                }
            }
        }
    }
}

private const val NOTE_PX = 64

@Composable
fun LarkCardTag(node: LarkCardNode) {
    val name = node.styleString("color")
    val hue = larkColor(LarkPalette.hue(name))
    Text(
        node.plainText, style = JTheme.type.labelMedium, color = if (name == "neutral" || name == "grey") JTheme.colors.secondaryText else hue,
        maxLines = 1, overflow = TextOverflow.Ellipsis,
        modifier = Modifier.clip(RoundedCornerShape(JSpace.xs)).background(hue.copy(alpha = 0.14f)).padding(horizontal = JSpace.sm, vertical = JSpace.xxs),
    )
}

@Composable
fun LarkCardIcon(node: LarkCardNode, size: Dp) {
    val sym = LarkCardIcons.symbol(node.token)
    val url = node.imageUrl
    when {
        sym != null -> Icon(LarkIcons.of(sym.first), null, Modifier.size(size),
            tint = (LarkPalette.text(node.styleString("color")) ?: sym.second)?.let { larkColor(it) } ?: JTheme.colors.secondaryText)
        url != null -> LarkRemoteImage(url, Modifier.size(size), size = thumbEdge(size), maxPx = ICON_PX, contentScale = ContentScale.Fit, placeholder = false)
        else -> Box(Modifier.size(size), contentAlignment = Alignment.Center) {
            Box(Modifier.size(size * 0.4f).clip(RoundedCornerShape(50)).background(JTheme.colors.secondaryText))
        }
    }
}

private const val ICON_PX = 96

/** Selects, pickers and inputs, shown read only with their current value (the options in a menu). */
@Composable
fun LarkCardField(node: LarkCardNode) {
    val options = node.raw["options"].arr.orEmpty()
    val selected = options.filter { it["selected"].bool == true }.mapNotNull { it["text"].str }
    val shown = if (selected.isNotEmpty()) selected.joinToString("、") else node.raw["value"].str?.takeIf { it.isNotEmpty() }
    var open by remember { mutableStateOf(false) }
    val c = JTheme.colors
    val shape = RoundedCornerShape(JRadius.badge)
    Box {
        Row(
            Modifier.fillMaxWidth().heightIn(min = JSize.wellM).clip(shape).background(c.label.copy(alpha = 0.03f)).border(JSize.hairline, LarkChrome.cardStroke, shape)
                .clickable(enabled = options.isNotEmpty()) { open = true }.padding(horizontal = JSpace.m).alpha(if (node.disabled) 0.6f else 1f),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(shown ?: node.text ?: "请选择", style = JTheme.type.secondary, color = if (shown == null) c.tertiaryText else c.label, maxLines = 1,
                overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            Icon(LarkIcons.field(node.tag), null, Modifier.size(JSize.markS), tint = c.secondaryText)
        }
        DropdownMenu(open, onDismissRequest = { open = false }) {
            for (o in options) DropdownMenuItem(
                text = { Text(o["text"].str ?: o["value"].str ?: "", maxLines = 1) }, onClick = { open = false },
                trailingIcon = if (o["selected"].bool == true) ({ Icon(JIcons.done, "已选") }) else null,
            )
        }
    }
}

/** `overflow`: the ••• menu; link options open, callback options press the card action. */
@Composable
fun LarkCardOverflow(node: LarkCardNode) {
    val actions = LocalLarkActions.current
    val ctx = LocalLarkContext.current
    val hub = LocalLarkHub.current
    val scope = rememberCoroutineScope()
    var open by remember { mutableStateOf(false) }
    val options = node.raw["options"].arr.orEmpty().filter { it["disabled"].bool != true }
    val shape = RoundedCornerShape(JRadius.badge)
    Box {
        Box(
            Modifier.size(JSize.well).clip(shape).border(JSize.hairline, LarkChrome.cardStroke, shape).clickable { open = true }.semantics { contentDescription = "更多" },
            contentAlignment = Alignment.Center,
        ) { Icon(JIcons.more, null, Modifier.size(JSize.badge), tint = JTheme.colors.label) }
        DropdownMenu(open, onDismissRequest = { open = false }) {
            for (o in options) {
                val text = o["text"].str ?: o["value"].str ?: ""
                val u = LarkFormat.safeUrl(o["url"].str)
                val aid = o["actionId"].str
                val mid = ctx.messageId
                DropdownMenuItem(
                    text = { Text(text, maxLines = 1) },
                    enabled = u != null || (aid != null && o["actionMethod"].int == 2 && mid != null && hub != null),
                    trailingIcon = if (u != null) ({ Icon(LarkIcons.outward, null) }) else null,
                    onClick = {
                        open = false
                        val api = hub?.api
                        if (u != null) actions.openUrl(u)
                        else if (aid != null && mid != null && api != null) scope.launch {
                            runCatching { api.larkCardAction(mid, aid) }
                                .onSuccess { Toasts.show("已提交「$text」", ToastStyle.SUCCESS); delay(REFRESH_AFTER_MS); actions.refresh?.invoke() }
                                .onFailure { Toasts.show("没提交成功：${it.message}", ToastStyle.ERROR) }
                        }
                    },
                )
            }
        }
    }
}

/** `collapsible_panel`: the header folds the body; children before a `collapse_divider` always show. */
@Composable
fun LarkCardPanel(node: LarkCardNode) {
    var open by rememberSaveable(node.raw.hashCode()) { mutableStateOf(node.raw["expanded"].bool ?: false) }
    val kids = node.children
    val divider = kids.indexOfFirst { it.tag == "collapse_divider" }
    val always = if (divider >= 0) kids.subList(0, divider) else emptyList()
    val folded = if (divider >= 0) kids.subList(divider + 1, kids.size) else kids
    val title = (if (open) node.node("expandedHeader") else null) ?: node.node("header")
    val bottom = node.raw["headerPosition"].str == "bottom"
    val iconLeft = node.raw["headerIconPosition"].str == "left"
    val header: @Composable () -> Unit = {
        Row(
            Modifier.fillMaxWidth().cardBox(node.raw["headerStyle"]).clickable(role = Role.Button) { open = !open },
            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm),
        ) {
            val chevron: @Composable () -> Unit = {
                Icon(LarkIcons.down, if (open) "收起" else "展开", Modifier.size(JSize.badge).rotate(if (open) 180f else 0f), tint = JTheme.colors.secondaryText)
            }
            if (iconLeft) chevron()
            Box(Modifier.weight(1f)) {
                if (title != null) LarkCardFlow(listOf(title)) else Text(node.text ?: "详情", style = JTheme.type.secondary, color = JTheme.colors.label)
            }
            if (!iconLeft) chevron()
        }
    }
    Column(Modifier.clipToBounds().animateContentSize(), verticalArrangement = Arrangement.spacedBy((LarkCardStyle.spacing(node) ?: 8f).dp)) {
        if (!bottom) header()
        if (always.isNotEmpty()) LarkCardFlow(always)
        if (open && folded.isNotEmpty()) LarkCardFlow(folded)
        if (bottom) header()
    }
}

/** Card tables: the header row tinted, every cell of a row as tall as its tallest; up to three columns share the width, wider tables scroll. */
@Composable
fun LarkCardTable(node: LarkCardNode) {
    val rows = node.children.filter { it.tag == "markdown_table_row" }
    val columns = rows.maxOfOrNull { it.children.size } ?: 0
    if (columns == 0) return
    val stroke = LarkChrome.cardStroke
    val shape = RoundedCornerShape(JRadius.badge)
    val wide = columns > 3
    Box(Modifier.then(if (wide) Modifier.horizontalScroll(rememberScrollState()) else Modifier.fillMaxWidth())) {
        Column(Modifier.then(if (wide) Modifier else Modifier.fillMaxWidth()).clip(shape).border(JSize.hairline, stroke, shape)) {
            rows.forEachIndexed { ri, row ->
                if (ri > 0) HorizontalDivider(color = stroke, thickness = JSize.hairline)
                Row(Modifier.height(IntrinsicSize.Min).then(if (wide) Modifier else Modifier.fillMaxWidth())) {
                    for (ci in 0 until columns) {
                        if (ci > 0) Box(Modifier.width(JSize.hairline).fillMaxHeight().background(stroke))
                        val cell = row.children.getOrNull(ci)
                        val isHeader = cell?.tag == "markdown_table_header"
                        Box(
                            Modifier.then(if (wide) Modifier.width(LarkSize.tableColumn) else Modifier.weight(1f)).fillMaxHeight()
                                .background(if (isHeader) JTheme.colors.label.copy(alpha = 0.05f) else Color.Transparent).padding(horizontal = JSpace.s, vertical = JSpace.sm),
                        ) {
                            if (cell != null) CompositionLocalProvider(LocalLarkTextBase provides LarkTextBase(size = 13f, bold = isHeader)) {
                                LarkCardFlow(if (cell.children.isEmpty()) listOf(cell) else cell.children, inMarkdown = true)
                            }
                        }
                    }
                }
            }
        }
    }
}

/** Charts in cards (VChart specs): bar / line / area / pie drawn directly; a placeholder otherwise. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LarkCardChart(spec: JsonElement?) {
    val c = JTheme.colors
    val data = spec["data"]
    val values = data.arr?.firstOrNull()?.get("values").arr ?: data["values"].arr ?: emptyList()
    val xf = spec["xField"].str ?: spec["categoryField"].str ?: "x"
    val yf = spec["yField"].str ?: spec["valueField"].str ?: "y"
    val sf = spec["seriesField"].str
    data class P(val x: String, val y: Double, val series: String)
    val pts = values.mapIndexedNotNull { i, v -> v[yf].dbl?.let { y -> P(v[xf].str ?: "$i", y, sf?.let { v[it].str } ?: "") } }
    val type = spec["type"].str ?: ""
    val palette = listOf("blue", "green", "orange", "purple", "turquoise", "red", "yellow", "indigo").map { larkColor(LarkPalette.fixed(it)) }
    val lineWidth = with(LocalDensity.current) { JSize.rule.toPx() }
    Column(verticalArrangement = Arrangement.spacedBy(JSpace.sm)) {
        spec["title"]["text"].str?.let { Text(it, style = JTheme.type.secondaryStrong, color = c.label) }
        if (pts.isEmpty()) {
            Row(Modifier.fillMaxWidth().heightIn(min = JSize.box80).clip(RoundedCornerShape(JRadius.control)).background(c.label.copy(alpha = 0.04f)),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.sm, Alignment.CenterHorizontally)) {
                Icon(LarkIcons.chart, null, Modifier.size(JSize.badge), tint = c.secondaryText)
                Text("图表", style = JTheme.type.secondary, color = c.secondaryText)
            }
            return@Column
        }
        val series = pts.map { it.series }.distinct()
        val xs = pts.map { it.x }.distinct()
        val grid = c.separator.copy(alpha = 0.3f)
        Canvas(Modifier.fillMaxWidth().height(LarkSize.chart).semantics { contentDescription = "图表" }) {
            if (type == "pie") {
                val total = pts.sumOf { maxOf(it.y, 0.0) }.takeIf { it > 0 } ?: return@Canvas
                val d = minOf(size.width, size.height)
                val topLeft = Offset((size.width - d) / 2, (size.height - d) / 2)
                var start = -90f
                pts.forEachIndexed { i, p ->
                    val sweep = (maxOf(p.y, 0.0) / total * 360).toFloat()
                    drawArc(palette[i % palette.size], start, sweep, useCenter = false, topLeft = topLeft + Offset(d * 0.15f, d * 0.15f),
                        size = Size(d * 0.7f, d * 0.7f), style = Stroke(width = d * 0.15f))
                    start += sweep
                }
                return@Canvas
            }
            val max = maxOf(pts.maxOf { it.y }, 0.0).takeIf { it > 0 } ?: 1.0
            for (k in 0..3) drawLine(grid, Offset(0f, size.height * k / 3), Offset(size.width, size.height * k / 3), strokeWidth = 1f)
            val slot = size.width / maxOf(xs.size, 1)
            series.forEachIndexed { si, s ->
                val color = palette[si % palette.size]
                val sp = pts.filter { it.series == s }
                if (type == "line" || type == "area") {
                    val path = Path()
                    sp.forEachIndexed { i, p ->
                        val x = slot * (xs.indexOf(p.x) + 0.5f)
                        val y = size.height * (1 - (p.y / max).toFloat())
                        if (i == 0) path.moveTo(x, y) else path.lineTo(x, y)
                    }
                    if (type == "area" && sp.isNotEmpty()) {
                        drawPath(Path().apply {
                            addPath(path)
                            lineTo(slot * (xs.indexOf(sp.last().x) + 0.5f), size.height)
                            lineTo(slot * (xs.indexOf(sp.first().x) + 0.5f), size.height)
                            close()
                        }, color.copy(alpha = 0.2f))
                    }
                    drawPath(path, color, style = Stroke(width = lineWidth))
                } else {
                    val bw = slot * 0.7f / maxOf(series.size, 1)
                    for (p in sp) {
                        val h = size.height * (p.y / max).toFloat()
                        drawRect(color, Offset(slot * xs.indexOf(p.x) + slot * 0.15f + bw * si, size.height - h), Size(bw * 0.9f, h))
                    }
                }
            }
        }
        if (type != "pie" && xs.size <= 8) Row(Modifier.fillMaxWidth()) {
            for (x in xs) Text(x, Modifier.weight(1f), style = JTheme.type.label, color = c.secondaryText, textAlign = TextAlign.Center, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (series.size > 1 || type == "pie") FlowRow(horizontalArrangement = Arrangement.spacedBy(JSpace.m)) {
            (if (type == "pie") pts.map { it.x } else series).forEachIndexed { i, l ->
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(JSpace.xs)) {
                    Box(Modifier.size(JSize.dot).clip(RoundedCornerShape(JSpace.xxs)).background(palette[i % palette.size]))
                    Text(l, style = JTheme.type.label, color = c.secondaryText, maxLines = 1)
                }
            }
        }
    }
}
