package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.material3.HorizontalDivider
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.TextStyle
import jarvis.android.ui.design.JRadius
import jarvis.android.ui.design.JSize
import jarvis.android.ui.design.JSpace
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.LinkAnnotation
import androidx.compose.ui.text.TextLinkStyles
import androidx.compose.ui.text.withLink
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import jarvis.android.ui.web.LinkSafety
import jarvis.android.ui.web.LinkText
import jarvis.android.ui.web.Links
import jarvis.android.ui.web.LocalLinkSource

/**
 * The Markdown Claude writes, enough of it to read comfortably: headings, lists (with a hanging indent), quotes,
 * fenced code, tables, rules, and inline **bold**, *italic*, `code` and [links](…). No dependency: a block parser
 * over lines plus an inline pass. Sizes and colors are the design system's (iOS: body 17 for replies, the first
 * heading level at title, the others at heading, code at code).
 */
@Composable
fun Markdown(
    text: String, modifier: Modifier = Modifier, color: Color = JTheme.colors.label,
    /** False where a long press means something else (the chat's message menu has 选择 instead). */
    selectable: Boolean = true,
    /** The paragraphs' level (replies are body; quiet text in a work line is secondary). */
    base: TextStyle = JTheme.type.body,
) {
    if (selectable) SelectionContainer(modifier) { MarkdownBody(text, Modifier, color, base) } else MarkdownBody(text, modifier, color, base)
}

@Composable
private fun MarkdownBody(text: String, modifier: Modifier, color: Color, base: TextStyle) {
    val blocks = remember(text) { parseBlocks(text) }
    // Links open in the app (CLAUDE.md「链接在 App 内打开」); the chat drawing the text is where page picks go back to.
    val from = LocalLinkSource.current
    val c = JTheme.colors
    val link = c.accent // the style's accent (Theme)
    val code = c.fillChip
    val styled = { s: String -> inline(s, link, code) { url -> Links.open(url, from) } }
    Column(modifier) {
        blocks.forEachIndexed { i, b ->
            val prev = blocks.getOrNull(i - 1)
            val top = when {
                i == 0 -> 0.dp
                b is Block.Heading -> JSpace.m
                b is Block.Item && prev is Block.Item -> JSpace.xs
                else -> JSpace.s
            }
            when (b) {
                is Block.Code -> Text(
                    b.text, style = JTheme.type.code, color = c.label, softWrap = false,
                    modifier = Modifier.padding(top = top).fillMaxWidth()
                        .background(c.codeBackground, RoundedCornerShape(JRadius.control))
                        .horizontalScroll(rememberScrollState()).padding(JSpace.m),
                )
                is Block.Heading -> Text(
                    styled(b.text), color = color,
                    style = if (b.level <= 1) JTheme.type.title else JTheme.type.heading,
                    modifier = Modifier.padding(top = top),
                )
                is Block.Para -> Text(styled(b.text), color = color, style = base, modifier = Modifier.padding(top = top))
                // The marker sits on the first line's baseline (CJK lines are taller than the marker's own).
                is Block.Item -> Row(Modifier.padding(top = top, start = JSpace.l * b.indent)) {
                    Text(b.marker, style = if (b.marker == "•") base.copy(fontWeight = androidx.compose.ui.text.font.FontWeight.Bold) else base,
                        color = if (b.marker == "•") c.secondaryText else color,
                        modifier = Modifier.alignByBaseline().widthIn(min = if (b.marker == "•") JSpace.l else JSpace.xl))
                    Text(styled(b.text), color = color, style = base, modifier = Modifier.alignByBaseline().weight(1f))
                }
                is Block.Quote -> Row(Modifier.padding(top = top).height(IntrinsicSize.Min)) {
                    Box(Modifier.width(JSize.bar).fillMaxHeight().background(c.fillPressed, RoundedCornerShape(JSize.bar)))
                    Text(styled(b.text), color = c.secondaryText, style = base, modifier = Modifier.padding(start = JSpace.m))
                }
                is Block.Table -> MarkdownTable(b, styled, base, Modifier.padding(top = top))
                Block.Rule -> HorizontalDivider(Modifier.padding(vertical = JSpace.s), thickness = JSize.hairline, color = c.separator)
            }
        }
    }
}

/** A table (iOS `MarkdownTable`): header in its own band, rows under hairlines, wider than the screen slides sideways. */
@Composable
private fun MarkdownTable(t: Block.Table, styled: (String) -> AnnotatedString, base: TextStyle, modifier: Modifier) {
    val c = JTheme.colors
    val shape = RoundedCornerShape(JRadius.control)
    Box(modifier.fillMaxWidth().horizontalScroll(rememberScrollState())) {
        Column(Modifier.clip(shape).border(JSize.hairline, c.separator, shape).width(IntrinsicSize.Max)) {
            val cols = maxOf(t.header.size, t.rows.maxOfOrNull { it.size } ?: 0)
            Row(Modifier.background(c.fillWell).height(IntrinsicSize.Min)) {
                for (i in 0 until cols) TableCell(styled(t.header.getOrElse(i) { "" }), base.copy(fontWeight = androidx.compose.ui.text.font.FontWeight.SemiBold), i > 0)
            }
            t.rows.forEach { r ->
                HorizontalDivider(thickness = JSize.hairline, color = c.separator)
                Row(Modifier.height(IntrinsicSize.Min)) {
                    for (i in 0 until cols) TableCell(styled(r.getOrElse(i) { "" }), base, i > 0)
                }
            }
        }
    }
}

@Composable
private fun RowScope.TableCell(text: AnnotatedString, style: TextStyle, divider: Boolean) {
    if (divider) Box(Modifier.width(JSize.hairline).fillMaxHeight().background(JTheme.colors.separator))
    Text(text, style = style.copy(fontSize = JTheme.type.secondary.fontSize), color = JTheme.colors.label,
        modifier = Modifier.weight(1f, fill = true).widthIn(min = JSize.box72, max = JSize.box220).padding(horizontal = JSpace.m, vertical = JSpace.s))
}

@Composable
private fun <T> remember(key: Any, calc: () -> T): T = androidx.compose.runtime.remember(key) { calc() }

sealed interface Block {
    data class Para(val text: String) : Block
    data class Heading(val level: Int, val text: String) : Block
    data class Code(val text: String) : Block
    data class Quote(val text: String) : Block
    /** One list item: its marker (• or 1.), how deep it sits, its words (wrapped lines hang under the words). */
    data class Item(val marker: String, val text: String, val indent: Int = 0) : Block
    data class Table(val header: List<String>, val rows: List<List<String>>) : Block
    data object Rule : Block
}

private val orderedItem = Regex("""^(\d{1,3})[.)]\s+(.*)$""")
private val tableRule = Regex("""^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$""")

private fun cells(line: String): List<String> = line.trim().removePrefix("|").removeSuffix("|").split('|').map { it.trim() }

/** Lines into blocks: paragraphs, headings, code, quotes, list items, tables, rules. */
fun parseBlocks(src: String): List<Block> {
    val out = mutableListOf<Block>()
    val para = StringBuilder()
    fun flush() { if (para.isNotBlank()) out += Block.Para(para.toString().trimEnd()); para.clear() }
    val lines = src.replace("\r\n", "\n").split('\n')
    var i = 0
    while (i < lines.size) {
        val line = lines[i]
        val t = line.trimStart()
        val indent = (line.length - t.length) / 2
        when {
            t.startsWith("```") -> {
                flush()
                val code = StringBuilder()
                i++
                while (i < lines.size && !lines[i].trimStart().startsWith("```")) { code.appendLine(lines[i]); i++ }
                out += Block.Code(code.toString().trimEnd('\n'))
            }
            t.startsWith("#") && t.trimStart('#').startsWith(" ") -> {
                flush(); out += Block.Heading(t.takeWhile { it == '#' }.length, t.trimStart('#').trim())
            }
            t.startsWith("> ") || t == ">" -> { flush(); out += Block.Quote(t.removePrefix(">").trim()) }
            t.isEmpty() -> flush()
            t.startsWith("|") && i + 1 < lines.size && tableRule.matches(lines[i + 1].trim()) -> {
                flush()
                val header = cells(t)
                i += 2
                val rows = mutableListOf<List<String>>()
                while (i < lines.size && lines[i].trimStart().startsWith("|")) { rows += cells(lines[i]); i++ }
                out += Block.Table(header, rows)
                continue
            }
            t.startsWith("- ") || t.startsWith("* ") || t.startsWith("+ ") -> { flush(); out += Block.Item("•", t.drop(2).trim(), indent) }
            orderedItem.matches(t) -> {
                flush()
                val m = orderedItem.find(t)!!
                out += Block.Item(m.groupValues[1] + ".", m.groupValues[2].trim(), indent)
            }
            t.matches(Regex("^-{3,}$|^\\*{3,}$")) -> { flush(); out += Block.Rule }
            // A line right under a list item, indented: it continues that item.
            out.lastOrNull() is Block.Item && para.isEmpty() && indent > 0 -> {
                val last = out.removeAt(out.lastIndex) as Block.Item
                out += last.copy(text = last.text + "\n" + t)
            }
            else -> { if (para.isNotEmpty()) para.append('\n'); para.append(line) }
        }
        i++
    }
    flush()
    return out
}

private val inlineRe = Regex(
    """\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)]\(([^)\s]+)\)|(?<![\w*])\*(?!\s)(.+?)(?<!\s)\*(?![\w*])|""" + LinkText.bareLink.pattern,
)

/** Links before the theme is known (unit tests): the glass style's accent. */
private val defaultLinkColor = Color(0xFF007AFF)
/** Inline code's soft gray before the theme is known. */
private val defaultCodeColor = Color(0x22888888)

/** A tappable [label] for [url] in [color] when there is an [onLink] handler; otherwise just coloured. */
private fun AnnotatedString.Builder.linkSpan(
    label: String, url: String, color: Color, onLink: ((String) -> Unit)?, underline: Boolean = false,
) {
    val style = SpanStyle(color = color, textDecoration = if (underline) TextDecoration.Underline else null)
    // javascript:, file:, content:… never open (LinkSafety): the label reads as plain text, not as a dead link.
    if (LinkSafety.kind(url) == LinkSafety.Kind.REFUSED) append(label)
    else if (onLink == null) withStyle(style) { append(label) }
    else withLink(LinkAnnotation.Url(url, TextLinkStyles(style)) { onLink(url) }) { append(label) }
}

/**
 * Inline Markdown; links are drawn in [link] (the style's accent). With [onLink], `[text](url)` and bare http(s)
 * URLs are tappable and go there.
 */
fun inline(s: String, link: Color = defaultLinkColor, code: Color = defaultCodeColor, onLink: ((String) -> Unit)? = null): AnnotatedString = buildAnnotatedString {
    var last = 0
    for (m in inlineRe.findAll(s)) {
        append(s.substring(last, m.range.first))
        val g = m.groups
        when {
            g[1] != null -> withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(g[1]!!.value) }
            g[2] != null -> withStyle(SpanStyle(fontFamily = FontFamily.Monospace, background = code)) { append(g[2]!!.value) }
            g[3] != null -> linkSpan(g[3]!!.value, g[4]!!.value, link, onLink)
            g[5] != null -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) { append(g[5]!!.value) }
            else -> {
                // A bare URL; prose punctuation after it is not part of it.
                val url = LinkText.trim(m.value)
                linkSpan(url, url, link, onLink)
                append(m.value.substring(url.length))
            }
        }
        last = m.range.last + 1
    }
    append(s.substring(last))
}

/** Plain text (what the user typed, an inbox message) with its bare http(s) URLs tappable. */
fun linkified(s: String, link: Color = defaultLinkColor, underline: Boolean = false, onLink: (String) -> Unit): AnnotatedString = buildAnnotatedString {
    var last = 0
    for (m in LinkText.bareLink.findAll(s)) {
        append(s.substring(last, m.range.first))
        val url = LinkText.trim(m.value)
        linkSpan(url, url, link, onLink, underline)
        append(m.value.substring(url.length))
        last = m.range.last + 1
    }
    append(s.substring(last))
}
