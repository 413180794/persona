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
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.withStyle
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The Markdown Claude writes, enough of it to read comfortably: headings, lists, quotes, fenced code, and inline
 * **bold**, *italic*, `code` and [links](…). No dependency: a block parser over lines plus an inline pass.
 */
@Composable
fun Markdown(text: String, modifier: Modifier = Modifier, color: Color = MaterialTheme.colorScheme.onSurface) {
    val blocks = remember(text) { parseBlocks(text) }
    val link = MaterialTheme.colorScheme.primary
    SelectionContainer(modifier) {
        Column {
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
                        inline(b.text, link), color = color, fontWeight = FontWeight.SemiBold,
                        style = if (b.level <= 2) MaterialTheme.typography.titleMedium else MaterialTheme.typography.titleSmall,
                        modifier = Modifier.padding(top = top + 2.dp),
                    )
                    is Block.Para -> Text(inline(b.text, link), color = color, style = MaterialTheme.typography.bodyLarge, modifier = Modifier.padding(top = top))
                    is Block.Quote -> Text(
                        inline(b.text, link), color = MaterialTheme.colorScheme.onSurfaceVariant, style = MaterialTheme.typography.bodyLarge,
                        modifier = Modifier.padding(top = top).padding(start = 10.dp),
                    )
                }
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

private val inlineRe = Regex("""\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)]\(([^)\s]+)\)|(?<![\w*])\*(?!\s)(.+?)(?<!\s)\*(?![\w*])""")

fun inline(s: String, link: Color = Color(0xFF3B82F6)): AnnotatedString = buildAnnotatedString {
    var last = 0
    for (m in inlineRe.findAll(s)) {
        append(s.substring(last, m.range.first))
        val g = m.groups
        when {
            g[1] != null -> withStyle(SpanStyle(fontWeight = FontWeight.SemiBold)) { append(g[1]!!.value) }
            g[2] != null -> withStyle(SpanStyle(fontFamily = FontFamily.Monospace, background = Color(0x22888888))) { append(g[2]!!.value) }
            g[3] != null -> withStyle(SpanStyle(color = link)) { append(g[3]!!.value) }
            g[5] != null -> withStyle(SpanStyle(fontStyle = FontStyle.Italic)) { append(g[5]!!.value) }
        }
        last = m.range.last + 1
    }
    append(s.substring(last))
}
