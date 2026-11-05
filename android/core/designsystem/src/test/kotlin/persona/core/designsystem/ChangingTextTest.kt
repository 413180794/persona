package persona.core.designsystem

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.SemanticsProperties
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/** A value drawn again with a new text (#2811): its numbers count, other text fades in, nothing moves when it shouldn't. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ChangingTextTest {
    @get:Rule val rule = createComposeRule()

    @Test fun numbersAreFoundAndWrittenAsTheValueWritesThem() {
        val a = NumberParts.of("¥1,234.50 / 08 项")
        assertEquals("¥# / # 项", a.shape)
        assertEquals(listOf(1234.5, 8.0), a.numbers)
        assertEquals("¥1,234.50 / 08 项", a.format(a.numbers))
        assertEquals("¥12,000.00 / 09 项", a.format(listOf(12000.0, 9.0)))
        assertEquals(NumberParts.of("2/8").shape, NumberParts.of("6/8").shape)
        assertNotEquals(NumberParts.of("在做").shape, NumberParts.of("2/8").shape)
        assertEquals("1.2M", NumberParts.of("8.4M").format(listOf(1.2)))
    }

    private fun text(): String = rule.onNodeWithTag("v").fetchSemanticsNode().config[SemanticsProperties.Text].joinToString("") { it.text }

    private fun draw(reduce: Boolean, first: String): (String) -> Unit {
        var value by mutableStateOf(first)
        rule.setContent {
            PersonaTheme(style = AppStyle.Brief, darkTheme = false, reduceMotion = reduce) {
                JChangingText(value, PersonaTheme.type.number, PersonaTheme.palette.ink, Modifier.testTag("v"))
            }
        }
        return { value = it }
    }

    @Test fun aChangedNumberCountsThroughWellWrittenValuesToTheNewOne() {
        rule.mainClock.autoAdvance = false
        val set = draw(reduce = false, first = "¥1.20")
        rule.mainClock.advanceTimeByFrame()
        assertEquals("¥1.20", text()) // the first draw does not move
        rule.runOnIdle { set("¥36.20") }
        val frames = (0 until 60).map { rule.mainClock.advanceTimeByFrame(); text() }
        assertTrue(frames.toString(), frames.all { Regex("""¥\d+\.\d{2}""").matches(it) })
        val numbers = frames.map { it.drop(1).toDouble() }
        assertTrue(frames.toString(), numbers.zipWithNext().all { (a, b) -> b >= a }) // counts up, never back
        assertTrue(frames.toString(), numbers.any { it > 1.2 && it < 36.2 })
        assertEquals("¥36.20", frames.last())
    }

    @Test fun withReducedMotionNoFrameShowsANumberInBetween() {
        rule.mainClock.autoAdvance = false
        val set = draw(reduce = true, first = "2/8")
        rule.mainClock.advanceTimeByFrame()
        rule.runOnIdle { set("6/8") }
        val frames = (0 until 10).map { rule.mainClock.advanceTimeByFrame(); text() }
        assertTrue(frames.toString(), frames.all { it == "2/8" || it == "6/8" })
        assertEquals("6/8", frames.last())
        assertEquals(frames.indexOfFirst { it == "6/8" }, frames.indexOfLast { it == "2/8" } + 1)
    }

    @Test fun anotherKindOfTextEndsAsTheNewText() {
        val set = draw(reduce = false, first = "在做")
        rule.waitForIdle()
        set("等你")
        rule.waitForIdle()
        assertEquals("等你", text())
        assertEquals(0, rule.onAllNodesWithText("在做").fetchSemanticsNodes().size)
    }
}
