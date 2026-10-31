package jarvis.android

import jarvis.android.data.JsonCodec
import jarvis.android.terminal.TermColor
import jarvis.android.terminal.TerminalFeed
import jarvis.android.terminal.TerminalGrid
import jarvis.android.terminal.TerminalKeys
import kotlinx.serialization.json.boolean
import kotlinx.serialization.json.int
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

/**
 * The terminal's VT subset, against herdr observe frames the iOS app's tests use (ios/JarvisTests/Fixtures): a scratch
 * pane (172×45) running a sample script — colors, a box with Chinese in it, an emoji, an OSC 8 link, a 200-character line.
 */
class TerminalTest {
    private fun observed(): TerminalGrid {
        val g = TerminalGrid(1, 1)
        javaClass.classLoader!!.getResource("terminal-observe.jsonl").readText().lines().filter { it.isNotBlank() }.forEach { line ->
            val f = JsonCodec.parseToJsonElement(line).jsonObject
            if (f["full"]!!.jsonPrimitive.boolean) g.reset(f["width"]!!.jsonPrimitive.int, f["height"]!!.jsonPrimitive.int)
            g.feed(Base64.getDecoder().decode(f["bytes"]!!.jsonPrimitive.content))
        }
        return g
    }

    @Test fun mirrorsWhatHerdrShows() {
        val g = observed()
        assertEquals(172, g.cols); assertEquals(45, g.rows)
        val text = g.lines.map(TerminalGrid::plain)
        assertEquals("$ sh /tmp/herdr-spike/sample.sh", text[0])
        assertEquals("✓ build ok  orange truecolor reverse under", text[1])
        assertEquals(listOf("┌──────────┐", "│ 中文对齐 │", "└──────────┘"), text.subList(2, 5))
        assertEquals("emoji 🚀 then text", text[5])
        assertEquals(200, text[7].length + text[8].length)
        assertEquals(setOf('L'), (text[7] + text[8]).toSet())
        assertEquals("$", text[9])
        assertTrue(g.cursorVisible); assertEquals(9, g.cursorRow); assertEquals(2, g.cursorCol)
    }

    @Test fun keepsColorsAndStyles() {
        val row = observed().lines[1]
        val s = row.joinToString("") { it.text }
        fun style(at: String) = row[s.indexOf(at)].style
        assertTrue(style("✓").bold); assertEquals(TermColor.Indexed(2), style("✓").fg)
        assertEquals(TermColor.Indexed(208), style("orange").fg)
        assertEquals(TermColor.Rgb(80, 160, 255), style("truecolor").fg)
        assertEquals(TermColor.Rgb(255, 255, 255), style("reverse").bg) // herdr sends reverse as its colors swapped in
        assertTrue(style("under").underline)
        assertFalse(style("  orange").bold)
    }

    @Test fun wideCharactersTakeTwoCells() {
        val g = observed()
        assertEquals("│", g.lines[3][11].text); assertEquals("┐", g.lines[2][11].text) // 中文对齐 is 4 characters in 8 cells
        val zhong = g.lines[3].indexOfFirst { it.text == "中" }
        assertEquals(2, g.lines[3][zhong].width); assertEquals(0, g.lines[3][zhong + 1].width)
        val rocket = g.lines[5].indexOfFirst { it.text == "🚀" }
        assertEquals(2, g.lines[5][rocket].width); assertEquals(" ", g.lines[5][rocket + 2].text)
    }

    @Test fun readsLinks() {
        val row = observed().lines[6]
        assertEquals("https://example.com/docs", row[0].style.link)
        assertEquals("https://example.com/docs", row[8].style.link)
        assertNull(row[10].style.link)
    }

    @Test fun scrollbackPagesAreRowsOfColoredText() {
        val rows = TerminalGrid.rowsFromAnsi("\u001B[0m\u001B[31mred\u001B[0m plain\r\n\r\nnext 中\r\n")
        assertEquals(listOf("red plain", "", "next 中"), rows.map(TerminalGrid::plain))
        assertEquals(TermColor.Indexed(1), rows[0][0].style.fg); assertEquals(TermColor.Standard, rows[0][4].style.fg)
        assertEquals(listOf("plain only"), TerminalGrid.rowsFromAnsi("plain only").map(TerminalGrid::plain))
        assertTrue(TerminalGrid.rowsFromAnsi("").isEmpty())
    }

    @Test fun erasesAndOverwrites() {
        val g = TerminalGrid(10, 3)
        g.feed("\u001B[2J\u001B[1;1Habcdef\u001B[1;3H\u001B[K")
        assertEquals("ab", TerminalGrid.plain(g.lines[0]))
        g.feed("\u001B[2;1H中文\u001B[2;2Hx") // writing into the right half of 中 blanks its left half
        assertEquals(" x文", TerminalGrid.plain(g.lines[1]))
        g.feed("\u001B[3;10H中") // a wide character that would cross the edge is not drawn
        assertEquals("", TerminalGrid.plain(g.lines[2]))
        g.feed("\u001B[?25l")
        assertFalse(g.cursorVisible)
        g.feed("\u001B[3;1H\u001B[41m\u001B[3X") // erase in a background color keeps the color
        assertTrue(g.lines[2].size >= 3 && g.lines[2].take(3).all { it.style.bg == TermColor.Indexed(1) })
    }

    @Test fun unknownSequencesAreSkipped() {
        val g = TerminalGrid(20, 2)
        g.feed("\u001B[?2026h\u001B(B\u001B=\u001B]0;title\u0007\u001B[5 qok\u001B[?2026l")
        assertEquals("ok", TerminalGrid.plain(g.lines[0]))
    }

    @Test fun sequencesMaySpanFeeds() {
        val g = TerminalGrid(20, 2)
        "\u001B[2;3Hhi\u001B]8;;https://x\u001B\\l".forEach { g.feed(it.toString()) }
        assertEquals("  hil", TerminalGrid.plain(g.lines[1]))
        assertEquals("https://x", g.lines[1][4].style.link)
    }

    @Test fun widths() {
        assertEquals(1, TerminalGrid.width('a'.code)); assertEquals(1, TerminalGrid.width('─'.code)); assertEquals(1, TerminalGrid.width(0x23FA)) // ⏺
        assertEquals(2, TerminalGrid.width('中'.code)); assertEquals(2, TerminalGrid.width('，'.code)); assertEquals(2, TerminalGrid.width("😀".codePointAt(0)))
        // A skin tone joins its emoji; U+FE0F makes ❤ emoji-wide.
        val g = TerminalGrid(10, 1)
        g.feed("👍🏽❤️x")
        assertEquals(listOf("👍🏽", "", "❤️", "", "x"), g.lines[0].map { it.text })
    }

    private fun d(s: String) = s.toByteArray()

    @Test fun framesApplyInOrder() {
        val f = TerminalFeed()
        assertEquals(TerminalFeed.Result.Gap, f.apply("e1", 3, false, 10, 2, d("x"))) // nothing to apply it to
        assertEquals(TerminalFeed.Result.Applied, f.apply("e1", 1, true, 10, 2, d("\u001B[2J\u001B[1;1Hab")))
        assertEquals(TerminalFeed.Result.Applied, f.apply("e1", 2, false, 10, 2, d("\u001B[1;3Hc")))
        assertEquals(TerminalFeed.Result.Duplicate, f.apply("e1", 2, false, 10, 2, d("\u001B[1;3HX")))
        assertEquals(listOf("abc"), f.content.value.rows.map(TerminalGrid::plain))
        assertEquals(TerminalFeed.Result.Gap, f.apply("e1", 4, false, 10, 2, d("y"))) // 3 went missing
        assertEquals(TerminalFeed.Result.Gap, f.apply("e1", 3, false, 10, 2, d("y"))) // dropped until a whole frame
        assertEquals(TerminalFeed.Result.Applied, f.apply("e2", 1, true, 20, 3, d("\u001B[2J\u001B[2;1Hnew")))
        assertEquals(20, f.content.value.cols)
        assertEquals(listOf("", "new"), f.content.value.rows.map(TerminalGrid::plain))
    }

    @Test fun theScreenEndsAtItsLastTextOrTheCursor() {
        val f = TerminalFeed()
        f.apply("e", 1, true, 10, 6, d("\u001B[2J\u001B[1;1H$ ls\u001B[2;1Ha\u001B[4;3H\u001B[?25h"))
        assertEquals(4, f.content.value.rows.size) // rows 5 and 6 are empty and below the cursor
        assertEquals(3 to 2, f.content.value.cursor)
        f.apply("e", 2, false, 10, 6, d("\u001B[?25l"))
        assertEquals(2, f.content.value.rows.size); assertNull(f.content.value.cursor)
    }

    @Test fun drivingKeepsWhatScrollsOffTheTop() {
        val f = TerminalFeed()
        f.setControl(TerminalFeed.Control.Active)
        fun screen(from: Int) = buildString { append("\u001B[2J"); for (r in 0 until 6) append("\u001B[${r + 1};1Hline ${from + r}") }
        f.apply("e", 1, true, 20, 6, d(screen(1)))
        f.apply("e", 2, false, 20, 6, d(screen(3))) // herdr redraws every row two lines up
        assertEquals((1..8).map { "line $it" }, f.content.value.rows.map(TerminalGrid::plain))
        assertEquals(2, f.content.value.screenStart)
        f.controlChanged("taken", null) // back to the pane's size: what was kept goes
        assertEquals(TerminalFeed.Control.Declined, f.control.value)
        f.apply("e", 3, false, 20, 6, d("\u001B[1;1Hx"))
        assertEquals(0, f.content.value.screenStart)
    }

    @Test fun keyBytesMatchIos() {
        assertEquals("\r", TerminalKeys.encode("enter"))
        assertEquals("\u001B\r", TerminalKeys.encode("shift+enter"))
        assertEquals("\u001B[A", TerminalKeys.encode("up"))
        assertEquals("\u001B[1;5D", TerminalKeys.encode("ctrl+left"))
        assertEquals("\u0003", TerminalKeys.encode("ctrl+c"))
        assertEquals("\u001B[Z", TerminalKeys.encode("shift+tab"))
        assertEquals("\u007F", TerminalKeys.encode("backspace"))
        assertEquals("\u001B[5~", TerminalKeys.encode("pageup"))
        assertEquals("\u001Bx", TerminalKeys.encode("alt+x"))
        assertEquals("1", TerminalKeys.encode("1"))
        assertEquals("ab", TerminalKeys.text("ab"))
        assertEquals("\u001B[200~a\rb\u001B[201~", TerminalKeys.text("a\nb"))
    }
}
