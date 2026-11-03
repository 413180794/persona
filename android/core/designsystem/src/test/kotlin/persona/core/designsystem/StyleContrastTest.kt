package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.math.pow

/**
 * The readability floors of the three styles (玻璃 / 晨报 / 看板) in light and dark, by WCAG contrast (the old app's
 * StylesTest, iOS's StyleSpecTests). Glass draws translucent fills: they are laid over the page background before
 * the contrast is measured.
 *
 * The first tests have no exceptions: body text on every surface, the solid action, the highlighter, the count
 * badge, the accent as a link and the user's own bubble. The last one also checks the quieter words and marks (status
 * words, the tertiary ink, words on a status fill, the board's holder glyphs), where some palettes are still short of
 * the floor: those are listed in [known] with their ratio today. The list may only shrink: a pair that gets worse, a new
 * one below its floor, and a listed one that now meets it all fail, so a palette fix has to remove its entry.
 */
class StyleContrastTest {
    private fun channel(c: Float): Double = if (c <= 0.04045f) c / 12.92 else ((c + 0.055) / 1.055).pow(2.4)

    private fun luminance(c: Color): Double = 0.2126 * channel(c.red) + 0.7152 * channel(c.green) + 0.0722 * channel(c.blue)

    /** [top] painted over an opaque [under]. */
    private fun over(top: Color, under: Color): Color {
        val a = top.alpha
        return Color(top.red * a + under.red * (1 - a), top.green * a + under.green * (1 - a), top.blue * a + under.blue * (1 - a), 1f)
    }

    /** The contrast of [text] drawn on [fill]; both may be translucent, the fill lies on [page]. */
    private fun ratio(text: Color, fill: Color, page: Color): Double {
        val back = over(fill, page)
        val front = over(text, back)
        val x = luminance(front) + 0.05
        val y = luminance(back) + 0.05
        return maxOf(x, y) / minOf(x, y)
    }

    private class Check(val name: String, val text: Color, val fill: Color, val floor: Double)

    private fun each(block: (style: AppStyle, dark: Boolean, palette: PersonaPalette) -> Unit) {
        for (style in AppStyle.entries) for (dark in listOf(false, true)) block(style, dark, Palettes.of(style, dark))
    }

    private fun label(style: AppStyle, dark: Boolean, check: Check) = "${style.name} ${if (dark) "dark" else "light"} ${check.name}"

    private fun below(checks: (PersonaPalette) -> List<Check>): List<String> {
        val found = mutableListOf<String>()
        each { style, dark, palette ->
            checks(palette).forEach { check ->
                val r = ratio(check.text, check.fill, palette.background)
                if (r < check.floor) found += "${label(style, dark, check)}: ${"%.2f".format(r)} < ${check.floor}"
            }
        }
        return found
    }

    /** The surfaces text sits on: the page, cards (translucent in glass), opaque panels, raised and sunken wells. */
    private fun surfaces(p: PersonaPalette) = listOf(
        "background" to p.background, "surface" to p.surface, "surfaceOpaque" to p.surfaceOpaque,
        "surfaceStrong" to p.surfaceStrong, "sunken" to p.sunken,
    )

    @Test fun bodyTextOnEverySurface() {
        val found = below { p ->
            surfaces(p).flatMap { (name, fill) ->
                listOf(Check("ink on $name", p.ink, fill, 4.5), Check("ink2 on $name", p.ink2, fill, 4.5))
            }
        }
        assertTrue("body text under 4.5:1: $found", found.isEmpty())
    }

    @Test fun solidButtonsTheHighlighterAndBadges() {
        val found = below { p ->
            listOf(
                Check("onAction on action", p.onAction, p.action, 4.5),
                Check("onMarker on marker", p.onMarker, p.marker, 4.5),
                Check("needsYou.onDot on needsYou.dot (count badges)", p.needsYou.onDot, p.needsYou.dot, 4.5),
            )
        }
        assertTrue("words on a solid fill under 4.5:1: $found", found.isEmpty())
    }

    @Test fun theAccentAsALink() {
        val found = below { p -> listOf(Check("accent on background", p.accent, p.background, 3.0), Check("accent on surface", p.accent, p.surface, 3.0)) }
        assertTrue("the accent under 3:1: $found", found.isEmpty())
    }

    @Test fun wordsOnTheUsersBubble() {
        // UserBubbleFrame: glass tints the bubble with the working light (28% in dark, 16% in light); the others use a card.
        val found = mutableListOf<String>()
        each { style, dark, p ->
            val bubble = if (style == AppStyle.Glass) p.working.dot.copy(alpha = if (dark) 0.28f else 0.16f) else p.surface
            val r = ratio(p.ink, bubble, p.background)
            if (r < 4.5) found += "${style.name} ${if (dark) "dark" else "light"}: ${"%.2f".format(r)}"
        }
        assertTrue("words on the user's bubble under 4.5:1: $found", found.isEmpty())
    }

    /**
     * Status words (StatusLabel, the request line) are captions: 4.5:1 for the states that ask for attention, 3:1 for the
     * quiet idle and offline ones. The tertiary ink (timestamps, hints) 3:1. Words on a status fill (a destructive
     * button, a swipe action) 3.5:1, iOS's floor for white on a solid accent. The board's holder glyphs are marks, 3:1.
     */
    private fun quieter(style: AppStyle, p: PersonaPalette): List<Check> = buildList {
        for ((name, colors, floor) in listOf(
            Triple("needsYou", p.needsYou, 4.5), Triple("working", p.working, 4.5), Triple("done", p.done, 4.5),
            Triple("danger", p.danger, 4.5), Triple("idle", p.idle, 3.0), Triple("offline", p.offline, 3.0),
        )) {
            add(Check("$name.text on background", colors.text, p.background, floor))
            add(Check("$name.text on surface", colors.text, p.surface, floor))
        }
        add(Check("ink3 on background", p.ink3, p.background, 3.0))
        add(Check("ink3 on surface", p.ink3, p.surface, 3.0))
        for ((name, colors) in listOf("danger" to p.danger, "done" to p.done, "working" to p.working)) {
            add(Check("$name.onDot on $name.dot", colors.onDot, colors.dot, 3.5))
        }
        if (style == AppStyle.Board) for ((name, colors) in listOf(
            "needsYou" to p.needsYou, "working" to p.working, "done" to p.done, "idle" to p.idle, "offline" to p.offline,
        )) add(Check("$name glyph on $name.dot", colors.onDot, colors.dot, 3.0))
    }

    /** Short of the floor today (2026-11-02), with the ratio then. Remove an entry when its palette is fixed. */
    private val known = mapOf(
        "Glass dark danger.onDot on danger.dot" to 2.77,
        "Glass dark working.onDot on working.dot" to 2.92,
        "Glass light needsYou.text on background" to 4.29,
        "Glass light done.text on background" to 4.28,
        "Glass light done.onDot on done.dot" to 3.14,
        "Brief light done.text on background" to 4.14,
        "Brief light done.text on surface" to 3.79,
        "Brief light offline.text on surface" to 2.87,
        "Brief light ink3 on surface" to 2.87,
        "Brief dark danger.onDot on danger.dot" to 2.79,
        "Board light done.text on background" to 4.24,
        "Board light offline.text on background" to 2.59,
        "Board light ink3 on background" to 2.59,
        "Board light done.onDot on done.dot" to 3.29,
        "Board light idle glyph on idle.dot" to 2.29,
        "Board light offline glyph on offline.dot" to 1.63,
        "Board dark danger.onDot on danger.dot" to 3.02,
        "Board dark working.onDot on working.dot" to 3.20,
    )

    @Test fun quieterWordsAndMarksKeepTheirFloorOrTheirKnownShortfall() {
        val problems = mutableListOf<String>()
        val seen = mutableSetOf<String>()
        each { style, dark, palette ->
            quieter(style, palette).forEach { check ->
                val name = label(style, dark, check)
                val r = ratio(check.text, check.fill, palette.background)
                val was = known[name]
                when {
                    was == null && r < check.floor -> problems += "$name: ${"%.2f".format(r)} < ${check.floor}"
                    was != null && r >= check.floor -> problems += "$name now meets ${check.floor}: remove it from known"
                    was != null && r < was - 0.02 -> problems += "$name got worse: ${"%.2f".format(r)} < $was"
                }
                if (was != null) seen += name
            }
        }
        (known.keys - seen).forEach { problems += "$it is not checked any more: remove it from known" }
        assertTrue(problems.joinToString("\n"), problems.isEmpty())
    }
}
