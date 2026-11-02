package jarvis.feature.terminal

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
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
import jarvis.core.contracts.api.TerminalCellPosition
import jarvis.core.contracts.api.TerminalSize
import jarvis.core.terminal.*
import kotlin.math.ceil
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min

/** Native drawing and IME bridge; touching a cell never opens the software keyboard. */
internal class TerminalCanvasView(context: Context, terminalText: Int, terminalBackground: Int) : View(context) {
    var onGrid: (TerminalSize) -> Unit = {}
    var onText: (String) -> Unit = {}
    var onKey: (String) -> Unit = {}
    var onHardware: (String) -> Unit = {}
    var onMarked: (String) -> Unit = {}
    var onWheel: (Int, TerminalCellPosition) -> Unit = { _, _ -> }
    var onCell: (TerminalCellPosition) -> Unit = {}
    private val composition = TerminalComposition({ onText(it) }, { onMarked(it); invalidate() })
    private val editable = SpannableStringBuilder()
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        typeface = runCatching { resources.getFont(R.font.jetbrains_mono) }.getOrDefault(Typeface.MONOSPACE)
        textSize = 15f * resources.displayMetrics.density
    }
    private val widths = LinkedHashMap<String, Float>()
    private val cellWidth = paint.measureText("M")
    private val cellHeight = ceil(paint.textSize * 1.42f)
    private val baseline = (cellHeight - paint.fontMetrics.bottom - paint.fontMetrics.top) / 2
    private val scroller = OverScroller(context)
    private var velocity: VelocityTracker? = null
    private var state = TerminalUiState()
    private var display = emptyList<DisplayLine>()
    private var perLine = 1
    private var offsetY = 0f
    private var offsetX = 0f
    private var downX = 0f
    private var downY = 0f
    private var lastX = 0f
    private var lastY = 0f
    private var dragged = false
    private var wheel: TerminalWheel? = null
    private var flingY = 0
    private var wrap = true
    private var foreground = terminalText
    private var background = terminalBackground
    private var _keyboard = false
    private val active get() = state.control == TerminalControl.Active && state.online
    private val slop = ViewConfiguration.get(context).scaledTouchSlop

    init {
        isFocusable = true; isFocusableInTouchMode = true
        contentDescription = "终端画面"
        importantForAutofill = IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS
    }
    fun render(value: TerminalUiState, wrap: Boolean, fg: Int, bg: Int) {
        val layoutChanged = state.lines !== value.lines || state.cursor != value.cursor || this.wrap != wrap || state.control != value.control
        if (state.control != value.control || state.online != value.online) {
            scroller.forceFinished(true)
            if (value.control == TerminalControl.Active) { offsetX = 0f; offsetY = 0f }
            else { composition.clear(); editable.clear() }
        }
        state = value; this.wrap = wrap; foreground = fg; background = bg
        if (layoutChanged) layout()
        invalidate()
    }
    fun keyboard(show: Boolean) {
        if (_keyboard == show) return
        _keyboard = show
        val manager = context.getSystemService(InputMethodManager::class.java)
        if (show && active) { requestFocus(); manager.restartInput(this); manager.showSoftInput(this, InputMethodManager.SHOW_IMPLICIT) }
        else { composition.clear(); editable.clear(); manager.hideSoftInputFromWindow(windowToken, 0); clearFocus() }
        invalidate()
    }
    override fun onDetachedFromWindow() {
        composition.clear(); editable.clear(); scroller.forceFinished(true); velocity?.recycle(); velocity = null
        super.onDetachedFromWindow()
    }
    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
        perLine = max(1, floor(w / cellWidth).toInt()); layout()
        val rows = floor(h / cellHeight).toInt()
        if (perLine >= 10 && rows >= 4) onGrid(TerminalSize(perLine, rows))
    }
    private fun layout() {
        display = TerminalWrap.lines(state.lines, state.cursor, perLine, wrap && state.control != TerminalControl.Active)
        clampOffsets()
    }
    private fun clampOffsets() {
        offsetY = offsetY.coerceIn(0f, max(0f, display.size * cellHeight - height))
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
                paint.color = fg; paint.style = Paint.Style.FILL; paint.alpha = if (cell.style.dim) 150 else 255
                paint.isFakeBoldText = cell.style.bold
                paint.textSkewX = if (cell.style.italic) -0.2f else 0f
                if (cell.text.isNotBlank() && !shape(canvas, cell.text, left, top, right)) {
                    val measured = widths.getOrPut(cell.text) { paint.measureText(cell.text) }
                    if (widths.size > 2048) widths.clear()
                    val scale = min(1f, (right - left) * (if (cell.width == 2) 0.92f else 1f) / measured.coerceAtLeast(1f))
                    canvas.save(); canvas.translate(left + (right - left - measured * scale) / 2, top + baseline)
                    canvas.scale(scale, 1f); canvas.drawText(cell.text, 0f, 0f, paint); canvas.restore()
                }
                if (cell.style.underline || cell.style.strike) paint.strokeWidth = lineWidth // box glyphs set their own
                if (cell.style.underline) canvas.drawLine(left, top + baseline + 2, right, top + baseline + 2, paint)
                if (cell.style.strike) canvas.drawLine(left, top + cellHeight / 2, right, top + cellHeight / 2, paint)
            }
            state.cursor?.takeIf { it.row == line.row && it.col in line.from until line.to }?.let { cursor ->
                val x = (cursor.col - line.from + line.indent) * cellWidth - offsetX
                paint.color = foreground; paint.alpha = if (_keyboard && active) 100 else 220; paint.strokeWidth = lineWidth
                paint.style = if (_keyboard && active) Paint.Style.FILL else Paint.Style.STROKE
                canvas.drawRect(x, top, x + cellWidth, top + cellHeight, paint)
                paint.style = Paint.Style.FILL; paint.alpha = 255; paint.isFakeBoldText = false; paint.textSkewX = 0f
                if (state.marked.isNotEmpty() && active) {
                    paint.color = background
                    canvas.drawRect(x, top, min(width.toFloat(), x + paint.measureText(state.marked)), top + cellHeight, paint)
                    paint.color = foreground
                    canvas.drawText(state.marked, x, top + baseline, paint)
                    canvas.drawLine(x, top + baseline + 2, min(width.toFloat(), x + paint.measureText(state.marked)), top + baseline + 2, paint)
                }
            }
        }
    }
    private fun shape(canvas: Canvas, text: String, left: Float, top: Float, right: Float): Boolean {
        if (text.length != 1) return false
        val ch = text[0]
        val bottom = top + cellHeight; val cx = (left + right) / 2; val cy = (top + bottom) / 2
        when (ch) {
            '█' -> canvas.drawRect(left, top, right, bottom, paint)
            '▀' -> canvas.drawRect(left, top, right, cy, paint)
            '▄' -> canvas.drawRect(left, cy, right, bottom, paint)
            '▌' -> canvas.drawRect(left, top, cx, bottom, paint)
            '▐' -> canvas.drawRect(cx, top, right, bottom, paint)
            in '▁'..'▇' -> canvas.drawRect(left, bottom - cellHeight * (ch.code - '▁'.code + 1) / 8, right, bottom, paint)
            else -> {
                val arms = when (ch) {
                    '─', '━', '═' -> "lr"; '│', '┃', '║' -> "ud"
                    '┌', '┏', '╔' -> "rd"; '┐', '┓', '╗' -> "ld"; '└', '┗', '╚' -> "ru"; '┘', '┛', '╝' -> "lu"
                    '├', '┣', '╠' -> "urd"; '┤', '┫', '╣' -> "uld"; '┬', '┳', '╦' -> "lrd"; '┴', '┻', '╩' -> "lru"; '┼', '╋', '╬' -> "lrud"
                    else -> return false
                }
                paint.strokeWidth = max(1f, paint.textSize / if (ch in "━┃┏┓┗┛┣┫┳┻╋") 8 else 14)
                if ('l' in arms) canvas.drawLine(left, cy, cx, cy, paint)
                if ('r' in arms) canvas.drawLine(cx, cy, right, cy, paint)
                if ('u' in arms) canvas.drawLine(cx, top, cx, cy, paint)
                if ('d' in arms) canvas.drawLine(cx, cy, cx, bottom, paint)
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
    private fun cell(x: Float, y: Float) = TerminalCellPosition(
        floor(x / cellWidth).toInt().coerceIn(0, max(0, state.cols - 1)),
        floor(y / cellHeight).toInt().coerceIn(0, max(0, state.rows - 1)))

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
                if (kotlin.math.abs(event.y - downY) > slop || kotlin.math.abs(event.x - downX) > slop) dragged = true
                if (dragged) {
                    if (active) wheel?.move(event.y - lastY, onWheel)
                    else { offsetY -= event.y - lastY; if (!wrap) offsetX -= event.x - lastX; clampOffsets(); invalidate() }
                }
                lastX = event.x; lastY = event.y; return true
            }
            MotionEvent.ACTION_UP -> {
                velocity?.addMovement(event)
                if (!dragged) { performClick(); if (active) onCell(cell(event.x, event.y)) }
                else if (active) {
                    velocity?.computeCurrentVelocity(1000)
                    flingY = 0
                    scroller.fling(0, 0, 0, velocity?.yVelocity?.toInt() ?: 0, 0, 0, -100_000, 100_000)
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
        if (active && scroller.computeScrollOffset()) {
            wheel?.move((scroller.currY - flingY).toFloat(), onWheel); flingY = scroller.currY
            postInvalidateOnAnimation()
        }
    }
    override fun performClick(): Boolean { super.performClick(); return true }
    override fun onCheckIsTextEditor() = active
    override fun onCreateInputConnection(outAttrs: EditorInfo): InputConnection {
        outAttrs.inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS or InputType.TYPE_TEXT_FLAG_MULTI_LINE
        outAttrs.imeOptions = EditorInfo.IME_FLAG_NO_EXTRACT_UI or EditorInfo.IME_FLAG_NO_PERSONALIZED_LEARNING
        outAttrs.initialSelStart = 0; outAttrs.initialSelEnd = 0
        return object : BaseInputConnection(this, true) {
            override fun getEditable(): Editable = editable
            override fun setComposingText(text: CharSequence?, newCursorPosition: Int): Boolean {
                if (!active) return false
                editable.replace(0, editable.length, text ?: "")
                setComposingSpans(editable)
                android.text.Selection.setSelection(editable, editable.length)
                composition.compose(text?.toString().orEmpty()); return true
            }
            override fun commitText(text: CharSequence?, newCursorPosition: Int): Boolean {
                if (!active) return false
                composition.commit(text?.toString().orEmpty()); editable.clear(); return true
            }
            override fun finishComposingText(): Boolean {
                if (active) composition.finish() else composition.clear()
                editable.clear(); return true
            }
            override fun deleteSurroundingText(beforeLength: Int, afterLength: Int): Boolean {
                if (!active) return false
                if (composition.delete(beforeLength)) editable.replace(0, editable.length, composition.marked)
                else repeat(beforeLength.coerceIn(0, 100)) { onKey("backspace") }
                return true
            }
            override fun deleteSurroundingTextInCodePoints(beforeLength: Int, afterLength: Int) = deleteSurroundingText(beforeLength, afterLength)
            override fun sendKeyEvent(event: KeyEvent) = dispatchKeyEvent(event)
            override fun performEditorAction(actionCode: Int): Boolean { if (active) onKey("enter"); return true }
        }
    }
    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (!active) return super.onKeyDown(keyCode, event)
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
            if (name == "backspace" && composition.delete(1)) editable.replace(0, editable.length, composition.marked)
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
