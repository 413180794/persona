package persona.core.terminal

/**
 * Box-drawing, block and a few symbol characters drawn as shapes filling their cell, as terminals do (iOS
 * `BoxDrawing.swift`): font glyphs for them rarely span the cell exactly (fallback fonts are narrower), and frames then
 * show as dashes with gaps. The bundled JetBrains Mono has no ⏺ ⏸ ⏵ ⏹ at all, and Android's emoji font takes them even
 * with U+FE0E, as a coloured tile two cells wide (Claude Code's message bullets and mode line).
 *
 * Pure geometry: coordinates are pixels of the cell rect given by the caller, the view only strokes and fills.
 */
object BoxDrawing {
    /** Arms from the cell's centre: 0 none, 1 light, 2 heavy, 3 double; [rounded] corners curve into the centre. */
    data class Arms(val up: Int = 0, val right: Int = 0, val down: Int = 0, val left: Int = 0, val rounded: Boolean = false)

    /** Part of a stroke, [width] pixels wide. A [Curve] is a quadratic from (x0, y0) through (cx, cy) to (x1, y1). */
    sealed interface Piece { val width: Float }
    data class Line(val x0: Float, val y0: Float, val x1: Float, val y1: Float, override val width: Float) : Piece
    data class Curve(val x0: Float, val y0: Float, val cx: Float, val cy: Float, val x1: Float, val y1: Float, override val width: Float) : Piece

    /** A block element: the part of the cell it fills, and how much of the colour (░ ▒ ▓ are shades). */
    data class Block(val left: Float, val top: Float, val right: Float, val bottom: Float, val alpha: Float)

    enum class Symbol { Circle, Square, Pause, Right, Left }

    private val table: Map<Int, Arms> = buildMap {
        // Dashed lines are drawn whole: a frame of them reads as a frame.
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

    fun arms(codePoint: Int): Arms? = table[codePoint]

    /** █ ▀ ▄ ▌ ▐ ░ ▒ ▓ ▁…▇ in the cell [left], [top], [right], [bottom]. */
    fun block(codePoint: Int, left: Float, top: Float, right: Float, bottom: Float): Block? {
        val cx = (left + right) / 2
        val cy = (top + bottom) / 2
        return when (codePoint) {
            0x2588 -> Block(left, top, right, bottom, 1f)
            0x2580 -> Block(left, top, right, cy, 1f)
            0x2584 -> Block(left, cy, right, bottom, 1f)
            0x258C -> Block(left, top, cx, bottom, 1f)
            0x2590 -> Block(cx, top, right, bottom, 1f)
            0x2591 -> Block(left, top, right, bottom, 0.25f)
            0x2592 -> Block(left, top, right, bottom, 0.5f)
            0x2593 -> Block(left, top, right, bottom, 0.75f)
            in 0x2581..0x2587 -> Block(left, bottom - (bottom - top) * (codePoint - 0x2580) / 8, right, bottom, 1f)
            else -> null
        }
    }

    /** ⏺ ● · ⏹ ■ · ⏸ · ⏵ ▶ ▸ · ⏴ ◀ ◂: drawn in the text colour, a little smaller than the cell. */
    fun symbol(codePoint: Int): Symbol? = when (codePoint) {
        0x23FA, 0x25CF -> Symbol.Circle
        0x23F9, 0x25A0 -> Symbol.Square
        0x23F8 -> Symbol.Pause
        0x23F5, 0x25B6, 0x25B8 -> Symbol.Right
        0x23F4, 0x25C0, 0x25C2 -> Symbol.Left
        else -> null
    }

    /** The symbol's side: 62 % of the cell's shorter side, as on the old Android app. */
    fun symbolSize(width: Float, height: Float): Float = minOf(width, height) * 0.62f

    /**
     * ✔ ✖ ✳ ✴ ❇ ✅ ❌ in one cell are text, not emoji: drawn with U+FE0E they stay in the cell's colour. (The ones above
     * are drawn as shapes instead: there U+FE0E does not help.)
     */
    fun textPresentation(codePoint: Int): Boolean = codePoint in textStyle
    private val textStyle = setOf(0x2714, 0x2716, 0x2733, 0x2734, 0x2747, 0x2705, 0x274C)

    /** A light line for a font size (in the same unit as the result): about a tenth of the size, in half steps. */
    fun lightLine(fontSize: Float): Float = maxOf(1f, Math.round(fontSize / 11f * 2f) / 2f)

    /**
     * The strokes of [arms] in the cell rect with a [light] line width: each arm runs from the edge past the centre by a
     * line, so corners close; heavy is twice as wide, double is two light lines a line apart either side of the centre.
     * The centre is rounded to the half-pixel grid, as on iOS.
     */
    fun pieces(arms: Arms, left: Float, top: Float, right: Float, bottom: Float, light: Float): List<Piece> {
        val cx = Math.round((left + right)) / 2f
        val cy = Math.round((top + bottom)) / 2f
        if (arms.rounded) {
            val radius = minOf(right - left, bottom - top) / 2
            val up = arms.up > 0
            val toRight = arms.right > 0
            val vertical = if (up) top else bottom
            val horizontal = if (toRight) right else left
            val nearY = cy + if (up) -radius else radius
            val nearX = cx + if (toRight) radius else -radius
            return listOf(
                Line(cx, vertical, cx, nearY, light),
                Curve(cx, nearY, cx, cy, nearX, cy, light),
                Line(nearX, cy, horizontal, cy, light),
            )
        }
        val out = mutableListOf<Piece>()
        fun arm(kind: Int, x0: Float, y0: Float, x1: Float, y1: Float, vertical: Boolean) {
            when (kind) {
                0 -> Unit
                3 -> for (d in floatArrayOf(-light, light)) {
                    out += if (vertical) Line(x0 + d, y0, x1 + d, y1, light) else Line(x0, y0 + d, x1, y1 + d, light)
                }
                else -> out += Line(x0, y0, x1, y1, if (kind == 2) light * 2 else light)
            }
        }
        arm(arms.up, cx, top, cx, cy + light, true)
        arm(arms.down, cx, cy - light, cx, bottom, true)
        arm(arms.left, left, cy, cx + light, cy, false)
        arm(arms.right, cx - light, cy, right, cy, false)
        return out
    }
}
