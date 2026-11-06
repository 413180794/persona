package persona.core.terminal

import java.io.File
import java.util.Base64
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.double
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The phone driving the terminal, on herdr's real frames of a 44 × 30 grid (`terminal session control`, recorded from
 * Claude Code 2.1.312 writing sixty numbered lines; the iOS app's `TerminalControlTests` replay the same file).
 */
class ScrollbackGridTest {
    private class Frame(val t: Double, val full: Boolean, val width: Int, val height: Int, val bytes: ByteArray)

    /**
     * The recording as the app gets it: frame by frame, or through a gate like the node's (frames within [gate]
     * seconds of the one sent before go out as one: the diffs run on, a whole frame drops what came before it).
     */
    private fun frames(gate: Double = 0.0): List<Frame> {
        val file = File(requireNotNull(System.getProperty("persona.repo.root")), "apple/PersonaTests/Fixtures/terminal-control-scroll.jsonl")
        val all = file.readLines().filter { it.startsWith("{") }.map { line ->
            val o = Json.parseToJsonElement(line).jsonObject
            Frame(o.getValue("t").jsonPrimitive.double, o.getValue("full").jsonPrimitive.boolean, o.getValue("width").jsonPrimitive.int,
                o.getValue("height").jsonPrimitive.int, Base64.getDecoder().decode(o.getValue("bytes").jsonPrimitive.content))
        }
        if (gate <= 0) return all
        val out = mutableListOf<Frame>()
        var pending: Frame? = null
        var due = 0.0
        var last = -1.0
        for (f in all) {
            pending?.let { if (f.t >= due) { out += it; last = due; pending = null } }
            val held = pending
            if (held != null) {
                pending = if (f.full) f else Frame(held.t, held.full, f.width, f.height, held.bytes + f.bytes)
            } else if (f.t - last >= gate) {
                out += f; last = f.t
            } else {
                pending = f; due = last + gate
            }
        }
        pending?.let { out += it }
        return out
    }

    private fun driven(gate: Double): ScrollbackGrid = ScrollbackGrid().also { grid ->
        frames(gate).forEach { grid.apply(it.full, it.width, it.height, it.bytes, keep = true) }
    }

    @Test fun whatScrolledOffIsKeptOnceInOrder() {
        // 0: as herdr sent them; 0.03: the node's gate; 0.25: much coarser, the screen moves several lines a frame.
        for (gate in listOf(0.0, 0.03, 0.25)) {
            val grid = driven(gate)
            assertTrue("gate $gate", grid.scrollbackSize > 0)
            val numbered = (grid.scrollback + grid.screen).map(TerminalGrid::plain).mapNotNull { line ->
                Regex("""^\D{0,4}?(\d+)\. \S""").find(line)?.groupValues?.get(1)?.toInt()
            }
            assertEquals("gate $gate", (1..60).toList(), numbered) // every line once, in order: none lost, none twice
        }
    }

    @Test fun rowsOfTheScreensWidthDoNotFold() {
        val grid = driven(0.0)
        val rows = grid.scrollback + grid.screen
        assertEquals(rows.size, TerminalWrap.lines(rows, null, 44, wrap = true).size)
    }

    @Test fun watchingKeepsNothingAndLettingGoDropsWhatWasKept() {
        val watching = ScrollbackGrid()
        frames().forEach { watching.apply(it.full, it.width, it.height, it.bytes, keep = false) }
        assertEquals(0, watching.scrollbackSize)
        val grid = driven(0.0)
        grid.forgetScrollback()
        assertEquals(0, grid.scrollbackSize)
    }

    @Test fun keepsAtMostTheLimitAndOnlyBetweenScreensOfOneWidth() {
        val grid = ScrollbackGrid(limit = 5)
        fun screen(from: Int) = buildString { append("\u001B[2J"); for (r in 0 until 6) append("\u001B[${r + 1};1Hline ${from + r}") }.toByteArray()
        grid.apply(true, 20, 6, screen(1), keep = true)
        for (n in 1..4) grid.apply(false, 20, 6, screen(1 + n * 2), keep = true) // two lines up each time
        assertEquals((4..8).map { "line $it" }, grid.scrollback.map(TerminalGrid::plain))
        grid.apply(true, 30, 6, screen(30), keep = true) // a new width is a new layout
        assertEquals(5, grid.scrollbackSize)
        assertEquals("line 30", TerminalGrid.plain(grid.screen.first()))
    }

    @Test fun aTallerScreenTakesBackTheRowsItShowsAgain() {
        // A program that shows the end of what it printed at whatever height: the keyboard comes up (fewer rows,
        // the top rows go to the scrollback), then goes down again (more rows: those rows are on screen again).
        val printed = (1..20).map { "line $it" }
        fun screen(rows: Int) = buildString {
            append("\u001B[2J")
            printed.takeLast(rows).forEachIndexed { r, line -> append("\u001B[${r + 1};1H$line") }
        }.toByteArray()
        val grid = ScrollbackGrid()
        grid.apply(true, 20, 12, screen(12), keep = true)
        grid.apply(true, 20, 8, screen(8), keep = true)
        assertEquals((9..12).map { "line $it" }, grid.scrollback.map(TerminalGrid::plain))
        grid.apply(true, 20, 12, screen(12), keep = true)
        assertEquals(0, grid.scrollbackSize)
        assertEquals((9..20).map { "line $it" }, grid.screen.map(TerminalGrid::plain))
        // Only rows that are on screen again go: older ones stay.
        val longer = ScrollbackGrid()
        longer.apply(true, 20, 12, ("\u001B[2J" + (1..12).joinToString("") { "\u001B[$it;1Hline $it" }).toByteArray(), keep = true)
        longer.apply(false, 20, 12, (1..12).joinToString("") { "\u001B[$it;1H\u001B[Kline ${it + 8}" }.toByteArray(), keep = true) // printed on: 9..20
        longer.apply(true, 20, 8, screen(8), keep = true)
        longer.apply(true, 20, 10, screen(10), keep = true)
        assertEquals((1..10).map { "line $it" }, longer.scrollback.map(TerminalGrid::plain))
    }

    @Test fun shownRowsEndAtTheLastTextOrTheCursor() {
        val grid = TerminalGrid(10, 6)
        grid.feed("\u001B[2J\u001B[1;1H$ ls\u001B[2;1Ha\u001B[4;3H")
        assertEquals(4, TerminalText.shownRows(grid.lines, grid.cursorRow)) // rows 5 and 6 are empty and below the cursor
        assertEquals(2, TerminalText.shownRows(grid.lines, null))
        assertEquals(0, TerminalText.shownRows(emptyList(), null))
    }

    @Test fun selectTextIsTheRowsAsPlainText() {
        val grid = TerminalGrid(20, 5)
        grid.feed("\u001B[2J\u001B[1;1H\u001B[31m$ ls\u001B[0m\u001B[2;1H中文 ok\u001B[4;1H")
        assertEquals("$ ls\n中文 ok", TerminalText.plain(grid.lines))
    }

    @Test fun selectTextShowsClaudesSymbolsWithGlyphsTheFontHas() {
        // The bundled JetBrains Mono has none of ⏺ ⏹ ⏸ ⏵ ⏴: Android drew ⏺ as an emoji tile and ⏵ as an empty box.
        val text = "⏺ 读完了\n  ⏵⏵ accept edits on\n⏸ plan mode on · ⏹ ⏴"
        assertEquals("● 读完了\n  ▶▶ accept edits on\n∥ plan mode on · ■ ◀", TerminalText.readable(text))
        // One character for one: the columns of the page stay where the terminal had them.
        assertEquals(text.length, TerminalText.readable(text).length)
        val plain = "$ ls\n中文 ok"
        assertTrue(TerminalText.readable(plain) === plain)
    }
}
