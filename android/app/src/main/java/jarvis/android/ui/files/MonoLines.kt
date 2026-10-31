package jarvis.android.ui.files

import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/** The editors' diff / Git colors, light and dark (iOS `DiffText` palette). */
@Immutable
data class DiffColors(val add: Color, val del: Color, val changed: Color, val addFill: Color, val delFill: Color, val hunkFill: Color)

@Composable
fun diffColors(): DiffColors {
    val dark = isSystemInDarkTheme()
    val add = if (dark) Color(0xFF4ADE80) else Color(0xFF16A34A)
    val del = if (dark) Color(0xFFF87171) else Color(0xFFDC2626)
    val changed = if (dark) Color(0xFFFBBF24) else Color(0xFFD97706)
    return remember(dark) {
        DiffColors(add, del, changed, add.copy(alpha = 0.14f), del.copy(alpha = 0.14f), Color.Gray.copy(alpha = 0.14f))
    }
}

/** One line of monospaced text: a gutter (line numbers, ± sign), the text, and the line's background. */
@Immutable
data class MonoLine(val gutter: String, val text: String, val color: Color, val fill: Color, val gutterColor: Color)

/** Columns a line takes in a monospaced font: wide (CJK, emoji) characters count two, a tab four. */
fun monoColumns(s: String): Int {
    var n = 0
    var i = 0
    while (i < s.length) {
        val cp = s.codePointAt(i)
        n += if (cp == '\t'.code) 4 else if (cp >= 0x1100 && (cp <= 0x115F || cp in 0x2E80..0xA4CF || cp in 0xAC00..0xD7A3 ||
                cp in 0xF900..0xFAFF || cp in 0xFE30..0xFE4F || cp in 0xFF00..0xFF60 || cp in 0xFFE0..0xFFE6 || cp >= 0x1F300)
        ) 2 else 1
        i += Character.charCount(cp)
    }
    return n
}

/**
 * Monospaced lines that scroll both ways without wrapping (iOS `MonoTextView`), lazily: files of thousands of lines
 * and big diffs stay smooth. The list is as wide as its longest line, so line backgrounds run the full width.
 */
@Composable
fun MonoLines(lines: List<MonoLine>, modifier: Modifier = Modifier, fontSize: Int = 12, contentPadding: PaddingValues = PaddingValues(vertical = 6.dp)) {
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    val style = TextStyle(fontFamily = FontFamily.Monospace, fontSize = fontSize.sp, lineHeight = (fontSize + 5).sp)
    val cell = remember(fontSize) { measurer.measure("0000000000", style).size.width / 10f }
    val gutterCols = remember(lines) { lines.maxOfOrNull { it.gutter.length } ?: 0 }
    val textCols = remember(lines) { lines.maxOfOrNull { monoColumns(it.text) } ?: 0 }
    val width = with(density) { ((gutterCols + textCols + 3) * cell).toDp() } + 24.dp
    Box(modifier.horizontalScroll(rememberScrollState())) {
        SelectionContainer {
            LazyColumn(Modifier.width(width).fillMaxSize(), contentPadding = contentPadding) {
                itemsIndexed(lines) { _, l ->
                    Row(Modifier.fillMaxWidth().background(l.fill).padding(horizontal = 8.dp)) {
                        if (l.gutter.isNotEmpty()) Text(l.gutter, style = style, color = l.gutterColor, softWrap = false)
                        Text(l.text.ifEmpty { " " }, style = style, color = l.color, softWrap = false)
                    }
                }
            }
        }
    }
}

/** A text file as numbered lines. */
@Composable
fun numberedLines(text: String): List<MonoLine> {
    val label = MaterialTheme.colorScheme.onSurface
    val quiet = MaterialTheme.colorScheme.onSurfaceVariant.copy(alpha = 0.6f)
    return remember(text, label, quiet) {
        val src = text.split('\n')
        val w = src.size.toString().length
        src.mapIndexed { i, l -> MonoLine((i + 1).toString().padStart(w) + "  ", l.replace("\t", "    "), label, Color.Transparent, quiet) }
    }
}

/** A unified diff's lines: old / new numbers, ± sign, colored backgrounds (iOS `DiffText.attributed`). */
@Composable
fun diffLines(lines: List<DiffLine>): List<MonoLine> {
    val c = diffColors()
    val label = MaterialTheme.colorScheme.onSurface
    val secondary = MaterialTheme.colorScheme.onSurfaceVariant
    return remember(lines, c, label, secondary) {
        fun num(n: Int?) = (n?.toString() ?: "").padStart(5)
        lines.map { l ->
            val (mark, fill) = when (l.kind) {
                DiffLine.Kind.ADD -> "+" to c.addFill
                DiffLine.Kind.DEL -> "−" to c.delFill
                DiffLine.Kind.HUNK -> " " to c.hunkFill
                else -> " " to Color.Transparent
            }
            val quiet = l.kind == DiffLine.Kind.HUNK || l.kind == DiffLine.Kind.INFO
            val gutterColor = when (l.kind) {
                DiffLine.Kind.ADD -> c.add
                DiffLine.Kind.DEL -> c.del
                else -> secondary.copy(alpha = 0.6f)
            }
            MonoLine(num(l.old) + num(l.new) + " $mark ", l.text.replace("\t", "    "), if (quiet) secondary else label, fill, gutterColor)
        }
    }
}
