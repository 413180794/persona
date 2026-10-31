package jarvis.android.terminal

import android.content.Context
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardBackspace
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Keyboard
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.KeyboardArrowUp
import androidx.compose.material.icons.filled.KeyboardReturn
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.input.pointer.positionChange
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalSoftwareKeyboardController
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import jarvis.android.JarvisApp
import jarvis.android.data.Hub
import jarvis.android.data.SessionKey
import kotlinx.coroutines.delay
import kotlin.math.abs

/** The terminal's font size (sp), kept on the phone; the range and step are the iOS app's (设置 › 终端). */
internal const val FONT_DEFAULT = 13f
internal const val FONT_MIN = 7f
internal const val FONT_MAX = 20f
internal fun nextFontSize(now: Float, up: Boolean): Float = (now + if (up) 0.5f else -0.5f).coerceIn(FONT_MIN, FONT_MAX)
private fun fontSize(c: Context) = c.getSharedPreferences("terminal", Context.MODE_PRIVATE).getFloat("font_size", FONT_DEFAULT)
private fun setFontSize(c: Context, v: Float) = c.getSharedPreferences("terminal", Context.MODE_PRIVATE).edit().putFloat("font_size", v).apply()
private val BG = Color(TerminalPalette.BACKGROUND)
private val FG = Color(TerminalPalette.FOREGROUND)

/** 手机尺寸 (drive the terminal at the phone's grid, the default) or 电脑尺寸 (mirror the pane as the computer has it). */
private fun phoneSize(c: Context) = c.getSharedPreferences("terminal", Context.MODE_PRIVATE).getBoolean("phone_size", true)
private fun setPhoneSize(c: Context, on: Boolean) = c.getSharedPreferences("terminal", Context.MODE_PRIVATE).edit().putBoolean("phone_size", on).apply()

/**
 * The live terminal of a session, in place of its chat page (docs/android.md «实时终端»): herdr's frames drawn cell by cell,
 * typed keys straight in. Driving it, a drag is the terminal's mouse wheel and a tap a click there.
 */
@Composable
fun TerminalScreen(hub: Hub, key: SessionKey, title: String, onClose: () -> Unit) {
    run {
        val context = LocalContext.current
        val link = remember(key) { TerminalLink(hub, key, JarvisApp.instance.scope) }
        val lifecycle = LocalLifecycleOwner.current.lifecycle
        DisposableEffect(link, lifecycle) {
            // Only while the app is in front: leaving gives the pane back to the computer at once.
            val obs = LifecycleEventObserver { _, e ->
                when (e) {
                    Lifecycle.Event.ON_START -> link.start(phoneSize(context))
                    Lifecycle.Event.ON_STOP -> link.stop()
                    else -> {}
                }
            }
            lifecycle.addObserver(obs)
            if (lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) link.start(phoneSize(context))
            onDispose { lifecycle.removeObserver(obs); link.stop() }
        }
        BackHandler(onBack = onClose)
        // A new size is a new grid: when the phone drives the terminal, the programs in it lay themselves out again.
        var font by remember { mutableStateOf(fontSize(context)) }
        val onFont = { up: Boolean -> font = nextFontSize(font, up); setFontSize(context, font) }
        Surface(color = BG, contentColor = FG, modifier = Modifier.fillMaxSize()) {
            Column(Modifier.fillMaxSize().statusBarsPadding().navigationBarsPadding().imePadding()) {
                TopStrip(link, title, font, onFont, onClose)
                TerminalBody(link, font, Modifier.weight(1f))
                KeyBar(link)
            }
        }
        ControlPrompts(link)
    }
}

@Composable
private fun TopStrip(link: TerminalLink, title: String, font: Float, onFont: (Boolean) -> Unit, onClose: () -> Unit) {
    val context = LocalContext.current
    val control by link.feed.control.collectAsState()
    val note by link.feed.note.collectAsState()
    val content by link.feed.content.collectAsState()
    val failure by link.failure.collectAsState()
    val mode by link.mode.collectAsState()
    var menu by remember { mutableStateOf(false) }
    var selecting by remember { mutableStateOf<String?>(null) }
    selecting?.let { text -> SelectScreenText(text, onDismiss = { selecting = null }) }
    Row(Modifier.fillMaxWidth().height(48.dp).padding(start = 4.dp, end = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = onClose, Modifier.testTag("terminal-close")) { Icon(Icons.Default.Close, "关闭终端", tint = FG) }
        Column(Modifier.weight(1f)) {
            Text(title, fontSize = 13.sp, lineHeight = 16.sp, maxLines = 1, color = FG)
            val status = failure ?: when {
                mode == TerminalLink.Mode.Poll -> "每秒读一次屏幕"
                control == TerminalFeed.Control.Active -> "手机尺寸 · ${content.cols}列"
                control == TerminalFeed.Control.Starting -> "接管中…"
                note != null -> "只看 · $note"
                else -> "电脑尺寸 · ${content.cols}列"
            }
            Text(status, fontSize = 11.sp, lineHeight = 14.sp, maxLines = 1, color = if (failure != null) Color(0xFFF7768E) else FG.copy(alpha = 0.6f))
        }
        if (control == TerminalFeed.Control.Declined && link.wantsControl && link.canControl) {
            TextButton(onClick = { link.takeOver() }) { Text("接管", maxLines = 1) }
        }
        // As the chat's 选择: the screen (and what scrolled off it) as plain text, any part of it to select and copy.
        IconButton(onClick = { selecting = screenText(content) }) { Icon(Icons.Default.ContentCopy, "选择文本", tint = FG) }
        Box {
            TextButton(onClick = { menu = true }) { Text(if (link.wantsControl) "手机尺寸" else "电脑尺寸", maxLines = 1, fontSize = 12.sp) }
            DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                DropdownMenuItem(text = { Text("手机尺寸（接管终端）") }, onClick = { menu = false; setPhoneSize(context, true); link.setWantsControl(true) })
                DropdownMenuItem(text = { Text("电脑尺寸（只看）") }, onClick = { menu = false; setPhoneSize(context, false); link.setWantsControl(false) })
                androidx.compose.material3.HorizontalDivider()
                // The menu stays open so a few taps get there.
                Row(Modifier.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically) {
                    Text("字号 ${"%.1f".format(font)}", Modifier.weight(1f, fill = false).padding(end = 8.dp))
                    TextButton(onClick = { onFont(false) }, enabled = font > FONT_MIN) { Text("A−", maxLines = 1) }
                    TextButton(onClick = { onFont(true) }, enabled = font < FONT_MAX) { Text("A＋", maxLines = 1) }
                }
            }
        }
    }
}

/** What the terminal shows as plain text, a line per row, without the blank rows at the end. */
internal fun screenText(content: TerminalContent): String = content.rows.joinToString("\n") { TerminalGrid.plain(it) }.trimEnd()

/**
 * 选择文本 for the terminal: the chat's full-screen page (iOS selects on the grid itself), but rows stay rows —
 * monospace, not wrapped, scrolled sideways — so columns and box drawing line up as they did on screen.
 */
@Composable
private fun SelectScreenText(text: String, onDismiss: () -> Unit) {
    val ctx = LocalContext.current
    jarvis.android.ui.chat.FullScreenPage(
        title = "选择文本",
        onBack = onDismiss,
        actions = {
            TextButton(onClick = {
                jarvis.android.ui.media.MediaShare.copyText(ctx, text, "终端")
                jarvis.android.ui.chat.Toasts.show("已拷贝", jarvis.android.ui.chat.ToastStyle.SUCCESS)
                onDismiss()
            }) { Text("全部拷贝", maxLines = 1) }
        },
    ) { pad ->
        androidx.compose.foundation.text.selection.SelectionContainer(
            Modifier.fillMaxSize().padding(pad).verticalScroll(rememberScrollState()).horizontalScroll(rememberScrollState()),
        ) {
            Text(text, fontFamily = FontFamily.Monospace, fontSize = 13.sp, lineHeight = 17.sp, softWrap = false, modifier = Modifier.padding(16.dp))
        }
    }
}

/** Cell metrics of the terminal font at a pixel size. */
private class Cells(px: Float) {
    val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply { typeface = Typeface.MONOSPACE; textSize = px }
    val bold = Paint(paint).apply { typeface = Typeface.create(Typeface.MONOSPACE, Typeface.BOLD) }
    val fill = Paint()
    val w: Float = paint.measureText("M")
    val h: Float = paint.fontSpacing.let { kotlin.math.ceil(it) }
    val baseline: Float = -paint.fontMetrics.ascent + (h - paint.fontSpacing) / 2
    val line: Float = maxOf(1f, px / 13f)
}

@Composable
private fun TerminalBody(link: TerminalLink, font: Float, modifier: Modifier) {
    val content by link.feed.content.collectAsState()
    val control by link.feed.control.collectAsState()
    val loaded by link.loaded.collectAsState()
    val density = LocalDensity.current
    val cells = remember(density, font) { Cells(with(density) { font.sp.toPx() }) }
    val list = rememberLazyListState()
    val driving = control == TerminalFeed.Control.Active

    BoxWithConstraints(modifier.fillMaxWidth()) {
        val widthPx = with(density) { maxWidth.toPx() }
        val heightPx = with(density) { maxHeight.toPx() }
        val grid = GridSize((widthPx / cells.w).toInt(), (heightPx / cells.h).toInt())
        // The first size at once (the subscription waits for it), later ones (the keyboard) once they settle.
        var first by remember { mutableStateOf(true) }
        LaunchedEffect(grid) {
            if (!first) delay(250)
            first = false
            link.setGrid(grid)
        }
        // Follow the end: the list is reversed, so item 0 is the last row.
        val rows = content.rows
        LaunchedEffect(rows.size) { if (list.firstVisibleItemIndex <= 1) list.scrollToItem(0) }

        val gridWidth = with(density) { (maxOf(content.cols, grid.cols) * cells.w).toDp() }
        val wheel = Modifier.pointerInput(driving, link.canScroll, cells) {
            if (!driving) return@pointerInput
            awaitEachGesture {
                val down = awaitFirstDown(requireUnconsumed = false)
                val at = (down.position.x / cells.w).toInt() to rowAt(down.position.y, size.height.toFloat(), cells, content)
                var moved = 0f; var sent = 0; var dragged = false
                while (true) {
                    val ev = awaitPointerEvent()
                    val ch = ev.changes.firstOrNull { it.id == down.id } ?: break
                    if (!ch.pressed) break
                    moved += ch.positionChange().y
                    if (abs(moved) > cells.h / 2) dragged = true
                    if (dragged && link.canScroll) {
                        // Finger down = back in history (> 0), one line per row height.
                        val lines = (moved / cells.h).toInt()
                        if (lines != sent) { link.wheel(lines - sent, at); sent = lines }
                        ch.consume()
                    }
                }
                if (!dragged) link.click(at)
            }
        }
        Box(Modifier.fillMaxSize().then(if (driving) Modifier else Modifier.horizontalScroll(rememberScrollState()))) {
            LazyColumn(
                state = list, reverseLayout = true, userScrollEnabled = !(driving && link.canScroll),
                modifier = Modifier.width(gridWidth).fillMaxSize().then(wheel)
                    .semantics { contentDescription = "terminal-screen" },
            ) {
                val n = rows.size
                itemsIndexed(List(n) { rows[n - 1 - it] }, key = null) { i, row ->
                    val r = n - 1 - i
                    TermRowView(row, cells, if (content.cursor?.first == r) content.cursor?.second else null, gridWidth)
                }
            }
        }
        if (!loaded) Text("连接终端…", Modifier.align(Alignment.Center), color = FG.copy(alpha = 0.6f))
    }
}

/** The content row (0-based in the live screen, as a click wants it) under a y in the reversed list's box. */
private fun rowAt(y: Float, height: Float, cells: Cells, content: TerminalContent): Int {
    val fromBottom = ((height - y) / cells.h).toInt()
    val r = content.rows.size - 1 - fromBottom
    return (r - content.screenStart).coerceAtLeast(0)
}

@Composable
private fun TermRowView(row: TermRow, cells: Cells, cursorCol: Int?, width: androidx.compose.ui.unit.Dp) {
    val h = with(LocalDensity.current) { cells.h.toDp() }
    androidx.compose.foundation.Canvas(Modifier.width(width).height(h)) {
        drawIntoCanvas { cv -> drawRow(cv.nativeCanvas, row, cells, cursorCol) }
    }
}

/** Every character in its own cell (wide ones in two), so columns and Chinese line up. */
private fun drawRow(c: android.graphics.Canvas, row: TermRow, m: Cells, cursorCol: Int?) {
    val rect = RectF()
    for ((i, cell) in row.withIndex()) {
        if (cell.width == 0) continue
        val s = cell.style
        var fg = TerminalPalette.argb(s.fg, true)
        var bg = TerminalPalette.argb(s.bg, false)
        if (s.inverse) { val t = fg; fg = bg; bg = t }
        val x = i * m.w
        rect.set(x, 0f, x + m.w * cell.width, m.h)
        if (bg != TerminalPalette.BACKGROUND || s.inverse) { m.fill.color = bg; m.fill.style = Paint.Style.FILL; c.drawRect(rect, m.fill) }
        if (cursorCol == i) { m.fill.color = TerminalPalette.CURSOR; c.drawRect(rect, m.fill); fg = TerminalPalette.BACKGROUND }
        if (s.hidden || cell.text == " ") continue
        if (s.dim) fg = (fg and 0x00FFFFFF) or (0x99 shl 24)
        val cp = cell.text.codePointAt(0)
        if (cell.text.length <= 2 && BoxDrawing.draw(c, cp, rect, fg, m.line, m.fill)) continue
        val p = if (s.bold) m.bold else m.paint
        p.color = fg
        p.textSkewX = if (s.italic) -0.2f else 0f
        p.isUnderlineText = s.underline || s.link != null
        p.isStrikeThruText = s.strike
        // ✔ ✳ and friends as text, not emoji (U+FE0E), so they stay one cell in the foreground color.
        val text = if (cp in TEXT_STYLE && cell.width == 1) cell.text + "\uFE0E" else cell.text
        val gw = p.measureText(text)
        val room = m.w * cell.width
        if (gw > room * 1.05f) {
            c.save(); c.scale(room / gw, 1f, x, 0f); c.drawText(text, x, m.baseline, p); c.restore()
        } else {
            c.drawText(text, x + (room - gw) / 2, m.baseline, p)
        }
    }
    if (cursorCol != null && cursorCol >= row.size) {
        rect.set(cursorCol * m.w, 0f, (cursorCol + 1) * m.w, m.h)
        m.fill.color = TerminalPalette.CURSOR; c.drawRect(rect, m.fill)
    }
}

private val TEXT_STYLE = setOf(0x2714, 0x2716, 0x2733, 0x2734, 0x2747, 0x2705, 0x274C)

/** Keys that are not on the phone's keyboard, and the keyboard itself (Ctrl arms the next key). */
@Composable
private fun KeyBar(link: TerminalLink) {
    var ctrl by remember { mutableStateOf(false) }
    var typing by remember { mutableStateOf(false) }
    val focus = remember { FocusRequester() }
    val keyboard = LocalSoftwareKeyboardController.current
    // What the field holds is what was typed since the last return; each change sends only its difference from what
    // went before (new text, or backspaces). Composing text goes in as it is shown: Android keyboards compose every
    // word for their suggestions, so waiting for the commit would hold back each word until a space; pinyin shows in
    // the terminal while it is chosen and is replaced (backspaces, then the characters) when it is.
    var field by remember { mutableStateOf(TextFieldValue("")) }
    fun press(name: String) { link.key(if (ctrl) "ctrl+$name" else name); ctrl = false }

    Column(Modifier.fillMaxWidth().background(Color(0xFF16171B))) {
        if (typing) {
            BasicTextField(
                value = field,
                onValueChange = { v ->
                    val before = field.text
                    field = v
                    val now = v.text
                    if (now == before) return@BasicTextField
                    val common = before.commonPrefixWith(now)
                    repeat(before.length - common.length) { link.key("backspace") }
                    val typed = now.substring(common.length)
                    if (typed.isNotEmpty()) {
                        if (ctrl && typed.length == 1) {
                            press(typed.lowercase())
                            field = TextFieldValue(before.substring(0, common.length), TextRange(common.length)) // ctrl+x is not text
                        } else link.type(typed)
                    }
                    if (field.composition == null && field.text.length > 200) field = TextFieldValue("")
                },
                singleLine = true,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, imeAction = ImeAction.Send, autoCorrectEnabled = false),
                keyboardActions = KeyboardActions(onSend = { press("enter"); field = TextFieldValue("") }),
                textStyle = MaterialTheme.typography.bodySmall.copy(color = FG, fontFamily = FontFamily.Monospace),
                cursorBrush = SolidColor(FG),
                modifier = Modifier.fillMaxWidth().height(1.dp).focusRequester(focus).testTag("terminal-input"),
            )
            LaunchedEffect(Unit) { focus.requestFocus(); keyboard?.show() }
        }
        Row(Modifier.fillMaxWidth().padding(horizontal = 4.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Row(
            Modifier.weight(1f).horizontalScroll(rememberScrollState()),
            horizontalArrangement = Arrangement.spacedBy(2.dp), verticalAlignment = Alignment.CenterVertically,
        ) {
            KeyText("Esc") { press("esc") }
            KeyText("Tab") { press("tab") }
            KeyText("Ctrl", on = ctrl) { ctrl = !ctrl }
            KeyIcon(Icons.Default.KeyboardArrowUp, "上") { press("up") }
            KeyIcon(Icons.Default.KeyboardArrowDown, "下") { press("down") }
            KeyIcon(Icons.Default.KeyboardArrowLeft, "左") { press("left") }
            KeyIcon(Icons.Default.KeyboardArrowRight, "右") { press("right") }
            KeyText("^C") { link.key("ctrl+c"); ctrl = false }
            KeyText("⇧Tab") { press("shift+tab") }
            KeyIcon(Icons.AutoMirrored.Filled.KeyboardBackspace, "退格") { press("backspace") }
            KeyIcon(Icons.Default.KeyboardReturn, "回车") { press("enter") }
        }
            Spacer(Modifier.width(4.dp))
            KeyIcon(Icons.Default.Keyboard, if (typing) "收起键盘" else "键盘", on = typing) {
                typing = !typing
                if (!typing) keyboard?.hide()
            }
        }
    }
}

@Composable
private fun KeyText(label: String, on: Boolean = false, onClick: () -> Unit) {
    TextButton(
        onClick = onClick,
        modifier = Modifier.height(36.dp).background(if (on) FG.copy(alpha = 0.2f) else Color.Transparent, RoundedCornerShape(8.dp)),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 10.dp),
    ) { Text(label, color = FG, fontFamily = FontFamily.Monospace, fontSize = 13.sp, maxLines = 1) }
}

@Composable
private fun KeyIcon(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, on: Boolean = false, onClick: () -> Unit) {
    IconButton(onClick = onClick, modifier = Modifier.size(36.dp).background(if (on) FG.copy(alpha = 0.2f) else Color.Transparent, RoundedCornerShape(8.dp))) {
        Icon(icon, label, tint = FG)
    }
}

/** Another program holds the terminal: take it, or only watch. */
@Composable
private fun ControlPrompts(link: TerminalLink) {
    val control by link.feed.control.collectAsState()
    if (control == TerminalFeed.Control.Busy) {
        AlertDialog(
            onDismissRequest = { link.watchOnly("电脑上另一个程序在用这个终端") },
            title = { Text("电脑上另一个程序正在使用这个终端") },
            text = { Text("接管后那个程序会断开，终端按手机的尺寸排版；只看就按电脑上的样子看。") },
            confirmButton = { TextButton(onClick = { link.takeOver() }) { Text("接管", maxLines = 1) } },
            dismissButton = { TextButton(onClick = { link.watchOnly("电脑上另一个程序在用这个终端") }) { Text("只看", maxLines = 1) } },
        )
    }
}
