package jarvis.android.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.MaterialTheme
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
 * The Markdown Claude writes, enough of it to read comfortably: headings, lists, quotes, fenced code, and inline
 * **bold**, *italic*, `code` and [links](…). No dependency: a block parser over lines plus an inline pass.
 */
@Composable
fun Markdown(
    text: String, modifier: Modifier = Modifier, color: Color = MaterialTheme.colorScheme.onSurface,
    /** False where a long press means something else (the chat's message menu has 选择 instead). */
    selectable: Boolean = true,
) {
    if (selectable) SelectionContainer(modifier) { MarkdownBody(text, Modifier, color) } else MarkdownBody(text, modifier, color)
}

@Composable
private fun MarkdownBody(text: String, modifier: Modifier, color: Color) {
    val blocks = remember(text) { parseBlocks(text) }
    // Links open in the app (CLAUDE.md「链接在 App 内打开」); the chat drawing the text is where page picks go back to.
    val from = LocalLinkSource.current
    val link = MaterialTheme.colorScheme.primary // the style's accent (Theme)
    val styled = { s: String -> inline(s, link) { url -> Links.open(url, from) } }
    Column(modifier) {
        blocks.forEachIndexed { i, b ->
            val top = if (i == 0) 0.dp else 6.dp
            when (b) {
                is Block.Code -> Text(
                    b.text,
                    fontFamily = FontFamily.Monospace, fontSize = 12.sp, lineHeight = 16.sp,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    softWrap = false,
                    modifier = Modifier.padding(top = top).fillMaxWidth()
                        .background(MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.6f), RoundedCornerShape(8.dp))
                        .horizontalScroll(rememberScrollState()).padding(10.dp),
                )
                is Block.Heading -> Text(
                    styled(b.text), color = color, fontWeight = FontWeight.SemiBold,
                    style = if (b.level <= 2) MaterialTheme.typography.titleMedium else MaterialTheme.typography.titleSmall,
                    modifier = Modifier.padding(top = top + 2.dp),
                )
                is Block.Para -> Text(styled(b.text), color = color, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(top = top))
                is Block.Quote -> Text(
                    styled(b.text), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyLarge,
                    modifier = Modifier.padding(top = top).padding(start = 10.dp),
                )
            }
        }
    }
}

@Composable
private fun <T> remember(key: Any, calc: () -> T): T = androidx.compose.runtime.remember(key) { calc() }

sealed interface Block {
    data class Para(val text: String) : Block
    data class Heading(val level: Int, val text: String) : Block
    data class Code(val text: String) : Block
    data class Quote(val text: String) : Block
}

/** Lines into blocks; list items stay in their paragraph with a bullet so they wrap naturally. */
fun parseBlocks(src: String): List<Block> {
    val out = mutableListOf<Block>()
    val para = StringBuilder()
    fun flush() { if (para.isNotBlank()) out += Block.Para(para.toString().trimEnd()); para.clear() }
    val lines = src.replace("\r\n", "\n").split('\n')
    var i = 0
    while (i < lines.size) {
        val line = lines[i]
        val t = line.trimStart()
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
            t.startsWith("> ") || t == ">" -> { flush(); out += Block.Quote("▎ " + t.removePrefix(">").trim()) }
            t.isEmpty() -> flush()
            t.startsWith("- ") || t.startsWith("* ") || t.startsWith("+ ") -> {
                val indent = (line.length - t.length) / 2
                if (para.isNotEmpty()) para.append('\n')
                para.append("  ".repeat(indent)).append("• ").append(t.drop(2))
            }
            t.matches(Regex("^-{3,}$|^\\*{3,}$")) -> flush()
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

private val defaultLinkColor = Color(0xFF3B82F6)

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
fun inline(s: String, link: Color = defaultLinkColor, onLink: ((String) -> Unit)? = null): AnnotatedString = buildAnnotatedString {
    var last = 0
    for (m in inlineRe.findAll(s)) {
        append(s.substring(last, m.range.first))
        val g = m.groups
        when {
            g[1] != null -> withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(g[1]!!.value) }
            g[2] != null -> withStyle(SpanStyle(fontFamily = FontFamily.Monospace, background = Color(0x22888888))) { append(g[2]!!.value) }
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
