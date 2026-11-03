package persona.core.terminal

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Against the iOS app's BoxDrawing.swift (the same table and strokes) and the old Android app's symbols. */
class BoxDrawingTest {
    @Test fun everyFrameCharacterHasArms() {
        // Light, heavy and double frames, rounded corners, dashes, half lines: what Claude Code and TUIs draw.
        "─━│┃┌┐└┘├┤┬┴┼┏┓┗┛┣┫┳┻╋═║╔╗╚╝╠╣╦╩╬╭╮╰╯┄┅┆┇┈┉┊┋╌╍╎╏╴╵╶╷".forEach { assertNotNull("$it", BoxDrawing.arms(it.code)) }
        assertEquals(BoxDrawing.Arms(right = 1, down = 1, rounded = true), BoxDrawing.arms('╭'.code))
        assertEquals(BoxDrawing.Arms(up = 3, right = 3, down = 3, left = 3), BoxDrawing.arms('╬'.code))
        assertEquals(BoxDrawing.Arms(right = 1, left = 1), BoxDrawing.arms('┄'.code)) // dashes are drawn whole
        assertEquals(BoxDrawing.Arms(down = 1), BoxDrawing.arms('╷'.code))
        listOf('a', ' ', '⏺', '█', '中').forEach { assertNull("$it", BoxDrawing.arms(it.code)) }
    }

    @Test fun armsRunPastTheCentreSoCornersClose() {
        val pieces = BoxDrawing.pieces(BoxDrawing.arms('┼'.code)!!, 0f, 0f, 10f, 20f, light = 2f)
        assertEquals(
            listOf(
                BoxDrawing.Line(5f, 0f, 5f, 12f, 2f), BoxDrawing.Line(5f, 8f, 5f, 20f, 2f),
                BoxDrawing.Line(0f, 10f, 7f, 10f, 2f), BoxDrawing.Line(3f, 10f, 10f, 10f, 2f),
            ),
            pieces,
        )
        // Heavy is twice as wide; double is two light lines a line either side of the centre.
        assertEquals(listOf(4f, 4f), BoxDrawing.pieces(BoxDrawing.arms('━'.code)!!, 0f, 0f, 10f, 20f, 2f).map { it.width })
        assertEquals(
            listOf(BoxDrawing.Line(0f, 8f, 7f, 8f, 2f), BoxDrawing.Line(0f, 12f, 7f, 12f, 2f), BoxDrawing.Line(3f, 8f, 10f, 8f, 2f), BoxDrawing.Line(3f, 12f, 10f, 12f, 2f)),
            BoxDrawing.pieces(BoxDrawing.arms('═'.code)!!, 0f, 0f, 10f, 20f, 2f),
        )
        // The centre is on the half-pixel grid, as on iOS.
        val odd = BoxDrawing.pieces(BoxDrawing.arms('│'.code)!!, 0.3f, 0f, 9.4f, 20f, 1f).first() as BoxDrawing.Line
        assertEquals(5f, odd.x0)
    }

    @Test fun roundedCornersCurveIntoTheCentre() {
        val pieces = BoxDrawing.pieces(BoxDrawing.arms('╭'.code)!!, 0f, 0f, 10f, 20f, 1f)
        assertEquals(
            listOf(
                BoxDrawing.Line(5f, 20f, 5f, 15f, 1f),
                BoxDrawing.Curve(5f, 15f, 5f, 10f, 10f, 10f, 1f),
                BoxDrawing.Line(10f, 10f, 10f, 10f, 1f),
            ),
            pieces,
        )
        val bottomLeft = BoxDrawing.pieces(BoxDrawing.arms('╯'.code)!!, 0f, 0f, 10f, 20f, 1f)
        assertEquals(BoxDrawing.Curve(5f, 5f, 5f, 10f, 0f, 10f, 1f), bottomLeft[1])
    }

    @Test fun blocksFillTheirPartOfTheCell() {
        assertEquals(BoxDrawing.Block(0f, 0f, 10f, 20f, 1f), BoxDrawing.block('█'.code, 0f, 0f, 10f, 20f))
        assertEquals(BoxDrawing.Block(0f, 0f, 10f, 10f, 1f), BoxDrawing.block('▀'.code, 0f, 0f, 10f, 20f))
        assertEquals(BoxDrawing.Block(5f, 0f, 10f, 20f, 1f), BoxDrawing.block('▐'.code, 0f, 0f, 10f, 20f))
        assertEquals(BoxDrawing.Block(0f, 15f, 10f, 20f, 1f), BoxDrawing.block('▂'.code, 0f, 0f, 10f, 20f))
        assertEquals(0.5f, BoxDrawing.block('▒'.code, 0f, 0f, 10f, 20f)!!.alpha)
        assertNull(BoxDrawing.block('─'.code, 0f, 0f, 10f, 20f))
    }

    @Test fun claudeCodesSymbolsAreShapesAndCheckMarksAreText() {
        assertEquals(BoxDrawing.Symbol.Circle, BoxDrawing.symbol('⏺'.code))
        assertEquals(BoxDrawing.Symbol.Circle, BoxDrawing.symbol('●'.code))
        assertEquals(BoxDrawing.Symbol.Square, BoxDrawing.symbol('⏹'.code))
        assertEquals(BoxDrawing.Symbol.Pause, BoxDrawing.symbol('⏸'.code))
        assertEquals(BoxDrawing.Symbol.Right, BoxDrawing.symbol('⏵'.code))
        assertEquals(BoxDrawing.Symbol.Right, BoxDrawing.symbol('▸'.code))
        assertEquals(BoxDrawing.Symbol.Left, BoxDrawing.symbol('◀'.code))
        assertNull(BoxDrawing.symbol('❯'.code))
        assertTrue(BoxDrawing.textPresentation('✔'.code))
        assertTrue(BoxDrawing.textPresentation('✳'.code))
        assertFalse(BoxDrawing.textPresentation('⏺'.code))
        // They take one cell, so the shape fits the cell (the emoji font would make them two).
        "⏺⏸⏵⏹⏴●■▶▸◀◂✔✳".forEach { assertEquals("$it", 1, TerminalGrid.width(it.toString())) }
        assertEquals(6.2f, BoxDrawing.symbolSize(10f, 20f), 0.001f)
    }

    @Test fun lightLinesFollowTheFontSize() {
        assertEquals(1.5f, BoxDrawing.lightLine(15f)) // iOS: max(1, round(size / 11 × 2) / 2)
        assertEquals(1f, BoxDrawing.lightLine(7f))
        assertEquals(2f, BoxDrawing.lightLine(20f))
    }
}
