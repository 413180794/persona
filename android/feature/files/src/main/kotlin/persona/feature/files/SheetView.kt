package persona.feature.files

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.ScrollState
import androidx.compose.foundation.background
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import persona.core.contracts.api.SessionFileContent
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons
import java.util.Base64

/** A table file read for the viewer: its sheets, or what went wrong. */
internal sealed interface SheetLoad {
    data object Reading : SheetLoad
    class Failed(val reason: String) : SheetLoad
    class Ready(val sheets: List<TableSheet>) : SheetLoad
}

/** Reads a CSV / TSV (the file's text) or an xlsx (its bytes) into sheets; off the main thread. */
internal fun readSheets(file: SessionFileContent): SheetLoad = try {
    val text = file.text
    when {
        TableFiles.isDelimited(file.name) && text != null -> SheetLoad.Ready(listOf(Delimited.sheet(file.name, text)))
        TableFiles.isDelimited(file.name) && file.data != null ->
            SheetLoad.Ready(listOf(Delimited.sheet(file.name, String(Base64.getMimeDecoder().decode(file.data), Charsets.UTF_8))))
        file.data != null -> SheetLoad.Ready(Xlsx.read(Base64.getMimeDecoder().decode(file.data)))
        else -> SheetLoad.Failed("没有读到文件内容")
    }
} catch (e: SheetFailure) {
    SheetLoad.Failed(e.message.orEmpty())
} catch (e: IllegalArgumentException) {
    SheetLoad.Failed("文件内容不完整")
}

/**
 * A table file as a grid (docs/android.md「文件查看器」, iOS `TableFileView`): column letters across the top and row
 * numbers down the side stay put while the cells scroll both ways; a workbook with several sheets has a chip per sheet.
 * Tap a cell to see all of it in the bar below (with a copy button); long-press to copy the cell or its row.
 */
@Composable
internal fun SheetFile(file: SessionFileContent, contentPadding: PaddingValues, header: LazyListScope.() -> Unit) {
    val load by produceState<SheetLoad>(SheetLoad.Reading, file.path, file.data, file.text) {
        value = withContext(Dispatchers.Default) { readSheets(file) }
    }
    when (val state = load) {
        SheetLoad.Reading -> LazyColumn(contentPadding = contentPadding) { header(); item { JLoading() } }
        is SheetLoad.Failed -> LazyColumn(contentPadding = contentPadding) {
            header()
            item { JEmptyState("无法显示这个表格", if (file.truncated) "文件超过 2 MB，只读到了前面的部分。${state.reason}" else state.reason) }
        }
        is SheetLoad.Ready -> SheetGrid(state.sheets, file.truncated, contentPadding, header)
    }
}

@Composable
internal fun SheetGrid(sheets: List<TableSheet>, truncated: Boolean, contentPadding: PaddingValues, header: LazyListScope.() -> Unit) {
    var index by rememberSaveable(sheets.size) { mutableIntStateOf(0) }
    val sheet = sheets[index.coerceIn(0, sheets.lastIndex)]
    var selected by remember(sheet) { mutableStateOf<Pair<Int, Int>?>(null) }
    var menu by remember(sheet) { mutableStateOf<Pair<Int, Int>?>(null) }
    val horizontal = remember(sheet) { ScrollState(0) }
    val widths = columnWidths(sheet)
    val numberWidth = numberWidth(sheet)
    val rowHeight = rowHeight()
    val columns = rememberVisibleColumns(widths, horizontal)
    val DataRow: @Composable (Int, Boolean) -> Unit = { r, pinned ->
        SheetRow(numberWidth, rowHeight, horizontal, label = "${r + 1}", header = false, highlighted = selected?.first == r) {
            VisibleCells(columns) { c ->
                val text = sheet[r, c]
                SheetCell(text, widths[c], rowHeight, header = false, strong = pinned, numeric = looksNumeric(text), selected = selected == (r to c),
                    onClick = { selected = if (selected == (r to c)) null else r to c }, onLongClick = { menu = r to c })
            }
        }
    }
    val context = LocalContext.current
    val feedback = LocalAppFeedback.current
    val palette = PersonaTheme.palette
    Column(Modifier.fillMaxSize().padding(top = contentPadding.calculateTopPadding()).testTag("sheet")) {
        if (sheets.size > 1) Row(Modifier.fillMaxWidth().horizontalScroll(rememberScrollState()).padding(horizontal = pagePadding, vertical = Tokens.Space.s),
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            sheets.forEachIndexed { i, s -> JChip(s.name.ifEmpty { "Sheet${i + 1}" }, selected = i == index, onClick = { index = i }) }
        }
        LazyColumn(Modifier.weight(1f).fillMaxWidth()) {
            header()
            if (truncated) item("cut") { Notice("文件超过 2 MB，只显示了前面读到的行") }
            if (sheet.capped) item("capped") { Notice("表格太长，只显示了前 ${TableSheet.MAX_ROWS} 行") }
            if (sheet.rows.isEmpty()) {
                item("empty") { JEmptyState("这个工作表是空的", "没有任何单元格。") }
                return@LazyColumn
            }
            // The column letters and the file's first row (its header, as iOS pins it) stay at the top.
            stickyHeader("header") {
                // Opaque under the header's tints (glass ones are translucent): rows scrolling below don't show through.
                Column(Modifier.background(palette.background)) {
                    SheetRow(numberWidth, rowHeight, horizontal, label = "", header = true) {
                        VisibleCells(columns) { c -> SheetCell(TableSheet.columnName(c), widths[c], rowHeight, header = true, numeric = false, selected = selected?.second == c) }
                    }
                    if (sheet.rows.size > 1) DataRow(0, true)
                }
            }
            items(sheet.rows.size, key = { it }, contentType = { "row" }) { r ->
                if (r > 0 || sheet.rows.size == 1) DataRow(r, false)
            }
        }
        SheetFooter(sheet, selected, truncated, Modifier.background(palette.surfaceOpaque).padding(bottom = contentPadding.calculateBottomPadding())) {
            copyText(context, it, feedback, "已拷贝单元格")
        }
    }
    menu?.let { (r, c) ->
        JActionSheet(listOf(
            JAction("拷贝单元格", PhIcons.Copy) { copyText(context, sheet[r, c], feedback, "已拷贝单元格") },
            JAction("拷贝整行", PhIcons.Copy) { copyText(context, sheet.rowText(r), feedback, "已拷贝整行") },
        ), { menu = null }, TableSheet.columnName(c) + (r + 1))
    }
}

/** A grid row: its number (or the corner) fixed on the left, the cells in the strip every row scrolls together. */
@Composable
private fun SheetRow(numberWidth: Dp, height: Dp, horizontal: ScrollState, label: String, header: Boolean, highlighted: Boolean = false,
    cells: @Composable RowScope.() -> Unit) {
    val palette = PersonaTheme.palette
    Row(Modifier.fillMaxWidth().background(if (header) palette.sunken else palette.background)) {
        Box(Modifier.size(numberWidth, height).background(if (highlighted) palette.accentSoft else palette.sunken).gridLines(palette.line)
            .padding(horizontal = Tokens.Space.xs), contentAlignment = Alignment.CenterEnd) {
            Text(label, style = PersonaTheme.type.caption, color = palette.ink2, maxLines = 1)
        }
        Row(Modifier.horizontalScroll(horizontal), content = cells)
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun SheetCell(text: String, width: Dp, height: Dp, header: Boolean, numeric: Boolean, selected: Boolean, strong: Boolean = false,
    onClick: (() -> Unit)? = null, onLongClick: (() -> Unit)? = null) {
    val palette = PersonaTheme.palette
    val fill = when {
        selected -> palette.accentSoft
        header -> palette.sunken
        else -> null
    }
    Box(Modifier.size(width, height).then(if (fill != null) Modifier.background(fill) else Modifier).gridLines(palette.line)
        .then(if (onClick != null) Modifier.combinedClickable(onLongClick = onLongClick, onClick = onClick) else Modifier)
        .semantics { if (text.isEmpty()) contentDescription = "空单元格" }
        .padding(horizontal = Tokens.Space.s),
        contentAlignment = if (header) Alignment.Center else if (numeric) Alignment.CenterEnd else Alignment.CenterStart) {
        Text(text.replace('\n', ' '), style = when { header -> PersonaTheme.type.caption; strong -> cellStyle().copy(fontWeight = FontWeight.SemiBold); else -> cellStyle() }, color = if (header) palette.ink2 else palette.ink,
            maxLines = 1, softWrap = false, overflow = TextOverflow.Ellipsis, textAlign = if (numeric) TextAlign.End else TextAlign.Start)
    }
}

/** The bar under the grid: the tapped cell's name and whole text with a copy button, else the sheet's size. */
@Composable
private fun SheetFooter(sheet: TableSheet, selected: Pair<Int, Int>?, truncated: Boolean, modifier: Modifier, copy: (String) -> Unit) {
    val palette = PersonaTheme.palette
    Row(modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
        if (selected != null) {
            val (r, c) = selected
            val text = sheet[r, c]
            Text(TableSheet.columnName(c) + (r + 1), style = PersonaTheme.type.label, color = palette.ink2, maxLines = 1)
            SelectionContainer(Modifier.weight(1f)) {
                Text(text.ifEmpty { " " }, style = PersonaTheme.type.callout, color = palette.ink, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
            JIconButton(PhIcons.Copy, "拷贝单元格", { copy(text) })
        } else {
            Text(sheetSummary(sheet, truncated), Modifier.weight(1f), style = PersonaTheme.type.label, color = palette.ink2,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

internal fun sheetSummary(sheet: TableSheet, truncated: Boolean): String = buildList {
    add("${sheet.rows.size} 行 × ${sheet.columns} 列")
    if (sheet.capped) add("只显示了前 ${TableSheet.MAX_ROWS} 行")
    else if (truncated) add("只读了前 2 MB")
}.joinToString(" · ")

@Composable private fun cellStyle(): TextStyle = PersonaTheme.type.callout

/** The right and bottom rule of a grid cell (one pixel). */
private fun Modifier.gridLines(color: Color) = drawBehind {
    val w = 1f
    drawLine(color, Offset(size.width - w / 2, 0f), Offset(size.width - w / 2, size.height), w)
    drawLine(color, Offset(0f, size.height - w / 2), Offset(size.width, size.height - w / 2), w)
}

/** Rows looked at to size the columns. */
private const val SAMPLE_ROWS = 300

/**
 * Column widths fitted to the widest text of the first rows (by character cells, CJK counting two), measured in the
 * cell font, between a narrow and a wide limit (long text is cut with …; tap the cell to read all of it).
 */
@Composable
private fun columnWidths(sheet: TableSheet): List<Dp> {
    val measurer = rememberTextMeasurer()
    val style = cellStyle().copy(fontWeight = FontWeight.SemiBold)
    val density = LocalDensity.current
    val pad = Tokens.Space.s * 2
    val min = Tokens.Space.xxxl * 2
    val max = Tokens.Space.xxxl * 7
    // One measurement for a character cell (half a CJK glyph, or a digit if wider), times each column's widest text in
    // cells: a sheet can have thousands of columns, too many to lay each one's text out on the main thread.
    val cell = remember(style, density) {
        with(density) { maxOf(measurer.measure("国", style).size.width / 2f, measurer.measure("0", style).size.width.toFloat()).toDp() }
    }
    return remember(sheet, cell, pad, min, max) {
        List(sheet.columns) { c ->
            var cells = textCells(TableSheet.columnName(c))
            for (r in 0 until minOf(SAMPLE_ROWS, sheet.rows.size)) {
                val t = sheet[r, c]
                if (t.isNotEmpty()) cells = maxOf(cells, textCells(if (t.length > 80) t.take(80) else t))
            }
            (cell * cells + pad).coerceIn(min, max)
        }
    }
}

/** The columns in view (and one either side), from the shared horizontal scroll; changes only when that range does. */
@Composable
private fun rememberVisibleColumns(widths: List<Dp>, horizontal: ScrollState): State<VisibleColumns> {
    val density = LocalDensity.current
    val view = with(density) { LocalConfiguration.current.screenWidthDp.dp.toPx() }
    val offsets = remember(widths, density) {
        with(density) { FloatArray(widths.size + 1).also { o -> widths.forEachIndexed { i, w -> o[i + 1] = o[i] + w.toPx() } } }
    }
    return remember(offsets, view) {
        derivedStateOf {
            val x = horizontal.value.toFloat()
            var first = 0
            while (first < widths.size - 1 && offsets[first + 1] < x) first++
            var last = first
            while (last < widths.size - 1 && offsets[last] < x + view) last++
            VisibleColumns((first - 1).coerceAtLeast(0), (last + 1).coerceAtMost(widths.size - 1), offsets)
        }
    }
}

/** Columns [first]..[last] of a row; the space of the ones before and after is kept so the row scrolls the same. */
internal class VisibleColumns(val first: Int, val last: Int, val offsets: FloatArray)

@Composable
private fun VisibleCells(columns: State<VisibleColumns>, cell: @Composable (Int) -> Unit) {
    val v = columns.value
    val density = LocalDensity.current
    if (v.offsets.size < 2) return
    with(density) {
        if (v.first > 0) Spacer(Modifier.width(v.offsets[v.first].toDp()))
        for (c in v.first..v.last) cell(c)
        val rest = v.offsets.last() - v.offsets[v.last + 1]
        if (rest > 0f) Spacer(Modifier.width(rest.toDp()))
    }
}

/** One height for every row (header, numbers, cells): a line of the cell font with room above and below. */
@Composable
private fun rowHeight(): Dp {
    val measurer = rememberTextMeasurer()
    val style = cellStyle()
    val density = LocalDensity.current
    return remember(style, density) { with(density) { measurer.measure("国Ag", style, maxLines = 1).size.height.toDp() } + Tokens.Space.s * 2 }
}

/** The row-number column: wide enough for the last row's number. */
@Composable
private fun numberWidth(sheet: TableSheet): Dp {
    val measurer = rememberTextMeasurer()
    val style = PersonaTheme.type.caption
    val density = LocalDensity.current
    return remember(sheet, style, density) {
        with(density) { measurer.measure("${maxOf(sheet.rows.size, 9)}", style).size.width.toDp() } + Tokens.Space.xs * 2 + Tokens.Space.xxs
    }
}

/** Display width of a text in character cells: wide CJK, kana, Hangul and full-width forms count two. */
internal fun textCells(s: String): Int {
    var n = 0
    var i = 0
    while (i < s.length) {
        val cp = s.codePointAt(i)
        n += if (cp in 0x1100..0x115F || cp in 0x2E80..0xA4CF || cp in 0xAC00..0xD7A3 || cp in 0xF900..0xFAFF ||
            cp in 0xFE30..0xFE4F || cp in 0xFF00..0xFF60 || cp in 0xFFE0..0xFFE6 || cp >= 0x1F300) 2 else 1
        i += Character.charCount(cp)
    }
    return n
}

private val numeric = Regex("^[-+(]?[$¥€£]?\\s?\\d[\\d,]*(\\.\\d+)?([eE][-+]?\\d+)?%?\\)?$")

/** A cell that reads as a number (right-aligned, like spreadsheets do): 1,234.5 · -3 · 12% · $4 · ¥4 · 1e5. */
internal fun looksNumeric(s: String): Boolean = s.isNotEmpty() && s.length < 40 && numeric.matches(s.trim())
