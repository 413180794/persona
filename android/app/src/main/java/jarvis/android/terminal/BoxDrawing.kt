package jarvis.android.terminal

import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RectF

/**
 * Box-drawing and block characters drawn as lines and rects filling their cell, like terminals do (the iOS app's
 * BoxDrawing.swift): font glyphs for them rarely span the cell exactly, and frames then show as dashes with gaps.
 */
object BoxDrawing {
    /** Arms from the cell's center (up, right, down, left): 0 none, 1 light, 2 heavy, 3 double; and rounded corners. */
    private class Arms(val up: Int = 0, val right: Int = 0, val down: Int = 0, val left: Int = 0, val rounded: Boolean = false)

    private val table: Map<Int, Arms> = buildMap {
        for (v in intArrayOf(0x2500, 0x2504, 0x2508, 0x254C)) put(v, Arms(right = 1, left = 1))
        for (v in intArrayOf(0x2501, 0x2505, 0x2509, 0x254D)) put(v, Arms(right = 2, left = 2))
        for (v in intArrayOf(0x2502, 0x2506, 0x250A, 0x254E)) put(v, Arms(up = 1, down = 1))
        for (v in intArrayOf(0x2503, 0x2507, 0x250B, 0x254F)) put(v, Arms(up = 2, down = 2))
        put(0x250C, Arms(right = 1, down = 1)); put(0x2510, Arms(down = 1, left = 1))
        put(0x2514, Arms(up = 1, right = 1)); put(0x2518, Arms(up = 1, left = 1))
        put(0x256D, Arms(right = 1, down = 1, rounded = true)); put(0x256E, Arms(down = 1, left = 1, rounded = true))
        put(0x2570, Arms(up = 1, right = 1, rounded = true)); put(0x256F, Arms(up = 1, left = 1, rounded = true))
        put(0x251C, Arms(up = 1, right = 1, down = 1)); put(0x2524, Arms(up = 1, down = 1, left = 1))
        put(0x252C, Arms(right = 1, down = 1, left = 1)); put(0x2534, Arms(up = 1, right = 1, left = 1))
        put(0x253C, Arms(1, 1, 1, 1))
        put(0x250F, Arms(right = 2, down = 2)); put(0x2513, Arms(down = 2, left = 2))
        put(0x2517, Arms(up = 2, right = 2)); put(0x251B, Arms(up = 2, left = 2))
        put(0x2523, Arms(up = 2, right = 2, down = 2)); put(0x252B, Arms(up = 2, down = 2, left = 2))
        put(0x2533, Arms(right = 2, down = 2, left = 2)); put(0x253B, Arms(up = 2, right = 2, left = 2))
        put(0x254B, Arms(2, 2, 2, 2))
        put(0x2550, Arms(right = 3, left = 3)); put(0x2551, Arms(up = 3, down = 3))
        put(0x2554, Arms(right = 3, down = 3)); put(0x2557, Arms(down = 3, left = 3))
        put(0x255A, Arms(up = 3, right = 3)); put(0x255D, Arms(up = 3, left = 3))
        put(0x2560, Arms(up = 3, right = 3, down = 3)); put(0x2563, Arms(up = 3, down = 3, left = 3))
        put(0x2566, Arms(right = 3, down = 3, left = 3)); put(0x2569, Arms(up = 3, right = 3, left = 3))
        put(0x256C, Arms(3, 3, 3, 3))
        put(0x2574, Arms(left = 1)); put(0x2575, Arms(up = 1)); put(0x2576, Arms(right = 1)); put(0x2577, Arms(down = 1))
    }

    /**
     * ⏺ ⏸ ⏵ ⏹ and friends drawn as shapes: Android's emoji font takes them even with U+FE0E, as a colored tile two
     * cells wide, where a terminal shows a one-cell symbol in the text color (Claude Code's bullets and mode line).
     */
    private fun symbol(c: Canvas, cp: Int, r: RectF, color: Int, p: Paint): Boolean {
        val s = minOf(r.width(), r.height()) * 0.62f
        val cx = r.centerX(); val cy = r.centerY()
        p.style = Paint.Style.FILL
        p.color = color
        when (cp) {
            0x23FA, 0x25CF -> c.drawCircle(cx, cy, s / 2, p) // ⏺ ●
            0x23F9, 0x25A0 -> c.drawRect(cx - s / 2, cy - s / 2, cx + s / 2, cy + s / 2, p) // ⏹ ■
            0x23F8 -> { val w = s / 3; c.drawRect(cx - s / 2, cy - s / 2, cx - s / 2 + w, cy + s / 2, p); c.drawRect(cx + s / 2 - w, cy - s / 2, cx + s / 2, cy + s / 2, p) } // ⏸
            0x23F5, 0x25B6, 0x25B8 -> c.drawPath(Path().apply { moveTo(cx - s / 2, cy - s / 2); lineTo(cx + s / 2, cy); lineTo(cx - s / 2, cy + s / 2); close() }, p) // ⏵ ▶ ▸
            0x23F4, 0x25C0, 0x25C2 -> c.drawPath(Path().apply { moveTo(cx + s / 2, cy - s / 2); lineTo(cx - s / 2, cy); lineTo(cx + s / 2, cy + s / 2); close() }, p) // ⏴ ◀ ◂
            else -> return false
        }
        return true
    }

    /** A block element as a part of the cell and how much of the color: █ ▀ ▄ ▌ ▐ ░ ▒ ▓ ▁…▇. */
    private fun block(v: Int, r: RectF): Pair<RectF, Float>? = when (v) {
        0x2588 -> RectF(r) to 1f
        0x2580 -> RectF(r.left, r.top, r.right, r.centerY()) to 1f
        0x2584 -> RectF(r.left, r.centerY(), r.right, r.bottom) to 1f
        0x258C -> RectF(r.left, r.top, r.centerX(), r.bottom) to 1f
        0x2590 -> RectF(r.centerX(), r.top, r.right, r.bottom) to 1f
        0x2591 -> RectF(r) to 0.25f
        0x2592 -> RectF(r) to 0.5f
        0x2593 -> RectF(r) to 0.75f
        in 0x2581..0x2587 -> RectF(r.left, r.bottom - r.height() * (v - 0x2580) / 8, r.right, r.bottom) to 1f
        else -> null
    }

    /** Draws cp filling r in color; false when it isn't one of these (draw the glyph instead). */
    fun draw(c: Canvas, cp: Int, r: RectF, color: Int, light: Float, p: Paint): Boolean {
        if (symbol(c, cp, r, color, p)) return true
        block(cp, r)?.let { (rect, alpha) ->
            p.style = Paint.Style.FILL
            p.color = color
            p.alpha = (alpha * android.graphics.Color.alpha(color)).toInt()
            c.drawRect(rect, p)
            p.alpha = 255
            return true
        }
        val a = table[cp] ?: return false
        p.style = Paint.Style.STROKE
        p.color = color
        p.strokeCap = Paint.Cap.BUTT
        val cx = r.centerX(); val cy = r.centerY()
        if (a.rounded) {
            val rad = minOf(r.width(), r.height()) / 2
            val path = Path()
            val up = a.up > 0; val right = a.right > 0
            path.moveTo(cx, if (up) r.top else r.bottom)
            path.lineTo(cx, cy + if (up) -rad else rad)
            path.quadTo(cx, cy, cx + if (right) rad else -rad, cy)
            path.lineTo(if (right) r.right else r.left, cy)
            p.strokeWidth = light
            c.drawPath(path, p)
            p.style = Paint.Style.FILL
            return true
        }
        val t = light
        fun arm(kind: Int, x0: Float, y0: Float, x1: Float, y1: Float, vertical: Boolean) {
            if (kind == 0) return
            if (kind == 3) {
                p.strokeWidth = t
                for (d in floatArrayOf(-t, t)) {
                    if (vertical) c.drawLine(x0 + d, y0, x1 + d, y1, p) else c.drawLine(x0, y0 + d, x1, y1 + d, p)
                }
                return
            }
            p.strokeWidth = if (kind == 2) t * 2 else t
            c.drawLine(x0, y0, x1, y1, p)
        }
        // Each arm runs past the center by half a line, so corners close.
        arm(a.up, cx, r.top, cx, cy + t, true)
        arm(a.down, cx, cy - t, cx, r.bottom, true)
        arm(a.left, r.left, cy, cx + t, cy, false)
        arm(a.right, cx - t, cy, r.right, cy, false)
        p.style = Paint.Style.FILL
        return true
    }
}
