package persona.feature.terminal

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.graphics.Typeface
import android.text.Editable
import android.text.InputType
import android.text.SpannableStringBuilder
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.VelocityTracker
import android.view.View
import android.view.ViewConfiguration
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputMethodManager
import android.widget.OverScroller
import persona.core.contracts.api.TerminalCellPosition
import persona.core.contracts.api.TerminalSize
import persona.core.terminal.*
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min

/**
 * Native drawing and IME bridge; touching a cell never opens the software keyboard.
 *
 * Driving the terminal, the live screen fills the view from the top and a drag is herdr's mouse wheel (or, on a node
 * without terminal_scroll, scrolls up into the rows the phone kept); watching it, the view follows the last line until
 * the user scrolls away from it.
 */
internal class TerminalCanvasView(context: Context, terminalText: Int, terminalBackground: Int) : View(context) {
    var onGrid: (TerminalSize) -> Unit = {}
    var onText: (String) -> Unit = {}
    var onKey: (String) -> Unit = {}
    var onHardware: (String) -> Unit = {}
    var onMarked: (String) -> Unit = {}
    var onWheel: (Int, TerminalCellPosition) -> Unit = { _, _ -> }
    var onCell: (TerminalCellPosition) -> Unit = {}
    /** A tap on an OSC 8 link: the URL, opened in the app (iOS); the tap is not also a click in the program. */
    var onLink: (String) -> Unit = {}
    private val composition = TerminalComposition({ onText(it) }, { onMarked(it); invalidate() }, { onKey(it) })
    // Not named `editable`: inside the input connection that name is its own getEditable(), which then called itself
    // until the stack overflowed as soon as an input method connected (2026-11-02).
    private val buffer = SpannableStringBuilder()
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        typeface = runCatching { resources.getFont(R.font.jetbrains_mono) }.getOrDefault(Typeface.MONOSPACE)
    }
    private val strokes = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE; strokeCap = Paint.Cap.BUTT }
    private val lightPath = Path()
    private val heavyPath = Path()
    private val shape = Path()
    private val widths = LinkedHashMap<String, Float>()
    private var fontSize = 0f
    private var cellWidth = 1f
    private var cellHeight = 1f
    private var baseline = 0f
    private var light = 1f
    private val scroller = OverScroller(context)
    private var velocity: VelocityTracker? = null
    private var state = TerminalUiState()
    private var display = emptyList<DisplayLine>()
    private var perLine = 1
    private var offsetY = 0f
    private var offsetX = 0f
    /** At the live end: the view moves with the content (the latest line watching, the screen's top driving). */
    private var follow = true
    private var downX = 0f
    private var downY = 0f
    private var lastX = 0f
    private var lastY = 0f
    private var dragged = false
    private var wheel: TerminalWheel? = null
    private var flingY = 0
    private var flingWheel = false
    private var wrap = true
    private var foreground = terminalText
    private var background = terminalBackground
    private var _keyboard = false
    private val driving get() = state.driving
    /** Typing goes to the hub whenever it is reachable (straight in while driving, through /keys while watching). */
    private val typing get() = state.online
    /** A drag is herdr's wheel only while driving a node that takes it. */
    private val wheelDrags get() = driving && state.wheel
    private val slop = ViewConfiguration.get(context).scaledTouchSlop

    init {
        isFocusable = true; isFocusableInTouchMode = true
        contentDescription = context.getString(R.string.terminal_screen_description)
        importantForAutofill = IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS
        font(TerminalFontSize.DEFAULT)
    }

    /** The font size in dp: new cells, so a new grid (a driven pane lays itself out again at the new size). */
    fun font(size: Float) {
        if (size == fontSize) return
        fontSize = size
        val density = resources.displayMetrics.density
        paint.textSize = size * density
        cellWidth = paint.measureText("M")
        cellHeight = ceil(paint.textSize * 1.42f)
        baseline = (cellHeight - paint.fontMetrics.bottom - paint.fontMetrics.top) / 2
        light = BoxDrawing.lightLine(size) * density
        widths.clear()
        if (width > 0 && height > 0) grid()
        invalidate()
    }

    fun render(value: TerminalUiState, wrap: Boolean, fg: Int, bg: Int) {
        val layoutChanged = state.lines !== value.lines || state.cursor != value.cursor || this.wrap != wrap ||
            state.control != value.control || state.screenStart != value.screenStart || state.online != value.online
        if (state.control != value.control || state.online != value.online) {
            scroller.forceFinished(true)
            if (value.driving != state.driving) { offsetX = 0f; follow = true }
            if (!value.online) { composition.clear(); buffer.clear() }
        }
        state = value; this.wrap = wrap; foreground = fg; background = bg
        if (layoutChanged) layout()
        invalidate()
    }
    fun keyboard(show: Boolean) {
        if (_keyboard == show) return
        _keyboard = show
        val manager = context.getSystemService(InputMethodManager::class.java)
        if (show && typing) { requestFocus(); manager.restartInput(this); manager.showSoftInput(this, InputMethodManager.SHOW_IMPLICIT) }
        else { composition.clear(); buffer.clear(); manager.hideSoftInputFromWindow(windowToken, 0); clearFocus() }
        invalidate()
    }
    override fun onDetachedFromWindow() {
        composition.clear(); buffer.clear(); scroller.forceFinished(true); velocity?.recycle(); velocity = null
        super.onDetachedFromWindow()
    }
    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) = grid()

    private fun grid() {
        perLine = max(1, floor(width / cellWidth).toInt()); layout()
        val rows = floor(height / cellHeight).toInt()
        if (perLine >= 10 && rows >= 4) onGrid(TerminalSize(perLine, rows))
    }
    private fun layout() {
        display = TerminalWrap.lines(state.lines, state.cursor, perLine, wrap && !driving)
        if (follow || wheelDrags) offsetY = liveOffset()
        clampOffsets()
    }
    /**
     * Where the view rests at the live end. Driving, the screen's first row is at the top (a screen taller than the
     * view, before the new grid lands, shows its bottom where the cursor is); watching, the last line is at the bottom.
     */
    private fun liveOffset(): Float {
        val bottom = max(0f, display.size * cellHeight - height)
        return if (driving) max(state.screenStart * cellHeight, bottom) else bottom
    }
    private fun maxOffsetY() = max(max(0f, display.size * cellHeight - height), if (driving) liveOffset() else 0f)
    private fun clampOffsets() {
        offsetY = offsetY.coerceIn(0f, maxOffsetY())
        offsetX = offsetX.coerceIn(0f, max(0f, state.cols * cellWidth - width))
    }

    override fun onDraw(canvas: Canvas) {
        canvas.drawColor(background)
        // Most cells are blanks on the default background: drawing nothing for them keeps a frame to the visible text.
        val lineWidth = max(1f, resources.displayMetrics.density)
        val first = max(0, floor(offsetY / cellHeight).toInt())
        val last = min(display.size, ceil((offsetY + height) / cellHeight).toInt())
        for (index in first until last) {
            val line = display[index]
            val row = state.lines.getOrNull(line.row) ?: continue
            val top = index * cellHeight - offsetY
            for (col in line.from until min(line.to, row.size)) {
                val cell = row[col]
                if (cell.width == 0) continue
                val left = (col - line.from + line.indent) * cellWidth - offsetX
                val right = left + cell.width * cellWidth
                var fg = color(cell.style.fg, foreground)
                var bg = color(cell.style.bg, background)
                if (cell.style.inverse) { val swap = fg; fg = bg; bg = swap }
                if (bg != background) {
                    paint.color = bg; paint.style = Paint.Style.FILL; paint.alpha = 255
                    canvas.drawRect(left, top, right, top + cellHeight, paint)
                }
                if (cell.style.hidden) continue
                val alpha = if (cell.style.dim) 150 else 255
                paint.color = fg; paint.style = Paint.Style.FILL; paint.alpha = alpha
                paint.isFakeBoldText = cell.style.bold
                paint.textSkewX = if (cell.style.italic) -0.2f else 0f
                if (cell.text.isNotBlank() && !shape(canvas, cell, left, top, right, fg, alpha)) {
                    val cp = cell.text.codePointAt(0)
                    // ✔ ✳ and the like as text (U+FE0E): one cell in the text colour, not a coloured emoji.
                    val text = if (cell.width == 1 && cell.text.length == Character.charCount(cp) && BoxDrawing.textPresentation(cp))
                        cell.text + "\uFE0E" else cell.text
                    val measured = widths.getOrPut(text) { paint.measureText(text) }
                    if (widths.size > 2048) widths.clear()
                    val scale = min(1f, (right - left) * (if (cell.width == 2) 0.92f else 1f) / measured.coerceAtLeast(1f))
                    canvas.save(); canvas.translate(left + (right - left - measured * scale) / 2, top + baseline)
                    canvas.scale(scale, 1f); canvas.drawText(text, 0f, 0f, paint); canvas.restore()
                }
                // OSC 8 links are underlined at half strength (iOS); a tap stays a click in the terminal.
                val link = cell.style.link != null && !cell.style.underline
                if (cell.style.underline || link || cell.style.strike) { paint.strokeWidth = lineWidth; paint.alpha = if (link) alpha / 2 else alpha }
                if (cell.style.underline || link) canvas.drawLine(left, top + baseline + 2, right, top + baseline + 2, paint)
                if (cell.style.strike) { paint.alpha = alpha; canvas.drawLine(left, top + cellHeight / 2, right, top + cellHeight / 2, paint) }
            }
            state.cursor?.takeIf { it.row == line.row && it.col in line.from until line.to }?.let { cursor ->
                val x = (cursor.col - line.from + line.indent) * cellWidth - offsetX
                paint.color = foreground; paint.alpha = if (_keyboard && typing) 100 else 220; paint.strokeWidth = lineWidth
                paint.style = if (_keyboard && typing) Paint.Style.FILL else Paint.Style.STROKE
                canvas.drawRect(x, top, x + cellWidth, top + cellHeight, paint)
                paint.style = Paint.Style.FILL; paint.alpha = 255; paint.isFakeBoldText = false; paint.textSkewX = 0f
                if (state.marked.isNotEmpty() && typing) {
                    paint.color = background
                    canvas.drawRect(x, top, min(width.toFloat(), x + paint.measureText(state.marked)), top + cellHeight, paint)
                    paint.color = foreground
                    canvas.drawText(state.marked, x, top + baseline, paint)
                    canvas.drawLine(x, top + baseline + 2, min(width.toFloat(), x + paint.measureText(state.marked)), top + baseline + 2, paint)
                }
            }
        }
        paint.alpha = 255; paint.isFakeBoldText = false; paint.textSkewX = 0f
    }

    /**
     * Frames, blocks and Claude Code's symbols as shapes filling the cell ([BoxDrawing]): font glyphs leave gaps in
     * frames, and the emoji font draws ⏺ ⏸ ⏵ as coloured tiles two cells wide. False: draw the glyph.
     */
    private fun shape(canvas: Canvas, cell: TermCell, left: Float, top: Float, right: Float, color: Int, alpha: Int): Boolean {
        val cp = cell.text.codePointAt(0)
        if (cell.text.length != Character.charCount(cp)) return false
        val bottom = top + cellHeight
        BoxDrawing.arms(cp)?.let { arms ->
            lightPath.reset(); heavyPath.reset()
            var lastPath: Path? = null
            var lastX = 0f
            var lastY = 0f
            for (piece in BoxDrawing.pieces(arms, left, top, right, bottom, light)) {
                // One path per width, stroked once: where arms overlap a dim colour does not show a darker seam.
                // Pieces that continue each other (a rounded corner) stay one contour.
                val path = if (piece.width > light) heavyPath else lightPath
                val (x0, y0) = when (piece) { is BoxDrawing.Line -> piece.x0 to piece.y0; is BoxDrawing.Curve -> piece.x0 to piece.y0 }
                if (path !== lastPath || x0 != lastX || y0 != lastY) path.moveTo(x0, y0)
                when (piece) {
                    is BoxDrawing.Line -> { path.lineTo(piece.x1, piece.y1); lastX = piece.x1; lastY = piece.y1 }
                    is BoxDrawing.Curve -> { path.quadTo(piece.cx, piece.cy, piece.x1, piece.y1); lastX = piece.x1; lastY = piece.y1 }
                }
                lastPath = path
            }
            strokes.color = color; strokes.alpha = alpha
            if (!lightPath.isEmpty) { strokes.strokeWidth = light; canvas.drawPath(lightPath, strokes) }
            if (!heavyPath.isEmpty) { strokes.strokeWidth = light * 2; canvas.drawPath(heavyPath, strokes) }
            return true
        }
        BoxDrawing.block(cp, left, top, right, bottom)?.let { block ->
            paint.alpha = (alpha * block.alpha).toInt()
            canvas.drawRect(block.left, block.top, block.right, block.bottom, paint)
            paint.alpha = alpha
            return true
        }
        val symbol = BoxDrawing.symbol(cp) ?: return false
        val s = BoxDrawing.symbolSize(right - left, cellHeight)
        val cx = (left + right) / 2
        val cy = top + cellHeight / 2
        when (symbol) {
            BoxDrawing.Symbol.Circle -> canvas.drawCircle(cx, cy, s / 2, paint)
            BoxDrawing.Symbol.Square -> canvas.drawRect(cx - s / 2, cy - s / 2, cx + s / 2, cy + s / 2, paint)
            BoxDrawing.Symbol.Pause -> {
                val bar = s / 3
                canvas.drawRect(cx - s / 2, cy - s / 2, cx - s / 2 + bar, cy + s / 2, paint)
                canvas.drawRect(cx + s / 2 - bar, cy - s / 2, cx + s / 2, cy + s / 2, paint)
            }
            BoxDrawing.Symbol.Right, BoxDrawing.Symbol.Left -> {
                val tip = if (symbol == BoxDrawing.Symbol.Right) 1 else -1
                shape.reset()
                shape.moveTo(cx - tip * s / 2, cy - s / 2); shape.lineTo(cx + tip * s / 2, cy); shape.lineTo(cx - tip * s / 2, cy + s / 2); shape.close()
                canvas.drawPath(shape, paint)
            }
        }
        return true
    }

    private fun color(value: TermColor, standard: Int): Int = when (value) {
        TermColor.Standard -> standard
        is TermColor.Rgb -> Color.rgb(value.red, value.green, value.blue)
        is TermColor.Indexed -> {
            val i = value.index.coerceIn(0, 255)
            when {
                i < 16 -> palette[i]
                i >= 232 -> (8 + (i - 232) * 10).let { Color.rgb(it, it, it) }
                else -> { val n = i - 16; fun part(v: Int) = if (v == 0) 0 else 55 + v * 40
                    Color.rgb(part(n / 36), part(n / 6 % 6), part(n % 6)) }
            }
        }
    }
    /** The live screen's cell under a point (rows kept above the screen count as its first row). */
    private fun cell(x: Float, y: Float) = TerminalCellPosition(
        floor((x + offsetX) / cellWidth).toInt().coerceIn(0, max(0, state.cols - 1)),
        (floor((y + offsetY) / cellHeight).toInt() - state.screenStart).coerceIn(0, max(0, state.rows - 1)))

    /** The OSC 8 link drawn at a point of the view, as laid out (wrapped lines included); the right half of a wide cell is its left. */
    private fun linkAt(x: Float, y: Float): String? {
        val line = display.getOrNull(floor((y + offsetY) / cellHeight).toInt()) ?: return null
        val row = state.lines.getOrNull(line.row) ?: return null
        var col = line.from + floor((x + offsetX) / cellWidth).toInt() - line.indent
        if (col < line.from || col >= min(line.to, row.size)) return null
        if (row[col].width == 0 && col > line.from) col--
        return row[col].style.link
    }

    override fun onTouchEvent(event: MotionEvent): Boolean {
        if (event.pointerCount != 1) { wheel = null; return true }
        when (event.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                parent?.requestDisallowInterceptTouchEvent(true)
                scroller.forceFinished(true)
                velocity?.recycle(); velocity = VelocityTracker.obtain().also { it.addMovement(event) }
                downX = event.x; lastX = event.x; downY = event.y; lastY = event.y; dragged = false
                wheel = TerminalWheel(cellHeight, cell(event.x, event.y)); return true
            }
            MotionEvent.ACTION_MOVE -> {
                velocity?.addMovement(event)
                if (abs(event.y - downY) > slop || abs(event.x - downX) > slop) dragged = true
                if (dragged) {
                    if (wheelDrags) wheel?.move(event.y - lastY, onWheel)
                    else {
                        offsetY -= event.y - lastY; if (!wrap && !driving) offsetX -= event.x - lastX
                        clampOffsets(); follow = offsetY >= maxOffsetY() - cellHeight / 2; invalidate()
                    }
                }
                lastX = event.x; lastY = event.y; return true
            }
            MotionEvent.ACTION_UP -> {
                velocity?.addMovement(event)
                if (!dragged) {
                    performClick()
                    val link = linkAt(event.x, event.y)
                    if (link != null) onLink(link) else if (driving) onCell(cell(event.x, event.y))
                }
                else {
                    velocity?.computeCurrentVelocity(1000)
                    val vx = velocity?.xVelocity?.toInt() ?: 0
                    val vy = velocity?.yVelocity?.toInt() ?: 0
                    flingWheel = wheelDrags
                    if (flingWheel) {
                        flingY = 0
                        scroller.fling(0, 0, 0, vy, 0, 0, -100_000, 100_000)
                    } else {
                        scroller.fling(offsetX.toInt(), offsetY.toInt(), if (!wrap && !driving) -vx else 0, -vy,
                            0, max(0f, state.cols * cellWidth - width).toInt(), 0, maxOffsetY().toInt())
                    }
                    postInvalidateOnAnimation()
                }
                velocity?.recycle(); velocity = null
                parent?.requestDisallowInterceptTouchEvent(false); return true
            }
            MotionEvent.ACTION_CANCEL -> { velocity?.recycle(); velocity = null; wheel = null; return true }
        }
        return true
    }
    override fun computeScroll() {
        if (!scroller.computeScrollOffset()) return
        if (flingWheel) {
            if (wheelDrags) wheel?.move((scroller.currY - flingY).toFloat(), onWheel)
            flingY = scroller.currY
        } else {
            offsetX = scroller.currX.toFloat(); offsetY = scroller.currY.toFloat()
            clampOffsets(); follow = offsetY >= maxOffsetY() - cellHeight / 2
        }
        postInvalidateOnAnimation()
    }
    override fun performClick(): Boolean { super.performClick(); return true }
    override fun onCheckIsTextEditor() = typing
    override fun onCreateInputConnection(outAttrs: EditorInfo): InputConnection {
        outAttrs.inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS or InputType.TYPE_TEXT_FLAG_MULTI_LINE
        outAttrs.imeOptions = EditorInfo.IME_FLAG_NO_EXTRACT_UI or EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING
        outAttrs.initialSelStart = 0; outAttrs.initialSelEnd = 0
        return object : BaseInputConnection(this, true) {
            override fun getEditable(): Editable = buffer
            override fun setComposingText(text: CharSequence?, newCursorPosition: Int): Boolean {
                if (!typing) return false
                buffer.replace(0, buffer.length, text ?: "")
                setComposingSpans(buffer)
                android.text.Selection.setSelection(buffer, buffer.length)
                composition.compose(text?.toString().orEmpty()); return true
            }
            override fun commitText(text: CharSequence?, newCursorPosition: Int): Boolean {
                if (!typing) return false
                composition.commit(text?.toString().orEmpty()); buffer.clear(); return true
            }
            override fun finishComposingText(): Boolean {
                if (typing) composition.finish() else composition.clear()
                buffer.clear(); return true
            }
            override fun deleteSurroundingText(beforeLength: Int, afterLength: Int): Boolean {
                if (!typing) return false
                if (composition.delete(beforeLength)) buffer.replace(0, buffer.length, composition.marked)
                else repeat(beforeLength.coerceIn(0, 100)) { onKey("backspace") }
                return true
            }
            override fun deleteSurroundingTextInCodePoints(beforeLength: Int, afterLength: Int) = deleteSurroundingText(beforeLength, afterLength)
            override fun sendKeyEvent(event: KeyEvent) = dispatchKeyEvent(event)
            override fun performEditorAction(actionCode: Int): Boolean { if (typing) onKey("enter"); return true }
        }
    }
    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (!typing) return super.onKeyDown(keyCode, event)
        if (keyCode in listOf(KeyEvent.KEYCODE_CTRL_LEFT, KeyEvent.KEYCODE_CTRL_RIGHT, KeyEvent.KEYCODE_ALT_LEFT,
                KeyEvent.KEYCODE_ALT_RIGHT, KeyEvent.KEYCODE_SHIFT_LEFT, KeyEvent.KEYCODE_SHIFT_RIGHT)) return true
        val name = when (keyCode) {
            KeyEvent.KEYCODE_ENTER -> "enter"; KeyEvent.KEYCODE_DEL -> "backspace"; KeyEvent.KEYCODE_FORWARD_DEL -> "delete"
            KeyEvent.KEYCODE_ESCAPE -> "esc"; KeyEvent.KEYCODE_TAB -> "tab"; KeyEvent.KEYCODE_DPAD_UP -> "up"
            KeyEvent.KEYCODE_DPAD_DOWN -> "down"; KeyEvent.KEYCODE_DPAD_LEFT -> "left"; KeyEvent.KEYCODE_DPAD_RIGHT -> "right"
            KeyEvent.KEYCODE_MOVE_HOME -> "home"; KeyEvent.KEYCODE_MOVE_END -> "end"
            KeyEvent.KEYCODE_PAGE_UP -> "pageup"; KeyEvent.KEYCODE_PAGE_DOWN -> "pagedown"
            else -> null
        }
        val prefix = listOfNotNull(if (event.isCtrlPressed) "ctrl" else null, if (event.isAltPressed) "alt" else null,
            if (event.isShiftPressed) "shift" else null).joinToString("+")
        if (name != null) {
            if (name == "backspace" && composition.delete(1)) buffer.replace(0, buffer.length, composition.marked)
            else if (prefix.isEmpty()) onKey(name) else onHardware("$prefix+$name")
            return true
        }
        val cp = event.getUnicodeChar(event.metaState and KeyEvent.META_CTRL_MASK.inv() and KeyEvent.META_ALT_MASK.inv())
        if (cp > 0 && cp and android.view.KeyCharacterMap.COMBINING_ACCENT == 0) {
            val text = String(Character.toChars(cp))
            if (event.isCtrlPressed || event.isAltPressed) onHardware("$prefix+${if (text == " ") "space" else text.lowercase()}") else onText(text)
            return true
        }
        return super.onKeyDown(keyCode, event)
    }
    companion object {
        private val palette = intArrayOf(0xff202124.toInt(), 0xffcc5555.toInt(), 0xff60a060.toInt(), 0xffbd9620.toInt(),
            0xff5d80cc.toInt(), 0xffae70bd.toInt(), 0xff3d9fa0.toInt(), 0xffd0d0d0.toInt(), 0xff777777.toInt(),
            0xffff7777.toInt(), 0xff8cce8c.toInt(), 0xffffd75f.toInt(), 0xff8caaff.toInt(), 0xffdca0eb.toInt(), 0xff80dede.toInt(), 0xfff5f5f5.toInt())
    }
}
