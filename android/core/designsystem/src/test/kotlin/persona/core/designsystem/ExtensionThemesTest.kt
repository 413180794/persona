package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.api.ExtensionTheme

class ExtensionThemesTest {
    private val theme = ExtensionTheme("example", "示例", "ocean", "海蓝", mapOf("accent" to "#0059A6"), mapOf("accent" to "#8BC4FF"))

    @Test fun validatesEverySurfaceInAllStylesNotJustWhiteAndBlack() {
        assertEquals(emptyList<String>(), ExtensionThemeColors.problems(theme))
        val marginal = theme.copy(light = mapOf("accent" to "#888888"), dark = mapOf("accent" to "#666666"))
        assertTrue(ExtensionThemeColors.problems(marginal).isNotEmpty())
        assertTrue(ExtensionThemeColors.problems(theme.copy(problems = listOf("hub 拒绝"))).isNotEmpty())
        assertTrue(ExtensionThemeColors.problems(theme.copy(light = mapOf("accent" to "not-a-color"))).isNotEmpty())
        assertTrue(ExtensionThemeColors.problems(theme.copy(dark = emptyMap())).isNotEmpty())
        assertTrue(ExtensionThemeColors.problems(theme.copy(light = theme.light + ("background" to "#000000"))).isNotEmpty())
    }

    @Test fun onlyDeclaredRolesChangeAndNothingChangesUntilChosen() {
        assertNull(ExtensionThemeColors.selected("hub-A", null, listOf(theme)))
        assertNull(ExtensionThemeColors.selected("hub-B", ExtensionThemeColors.selection("hub-A", theme), listOf(theme)))
        assertEquals(theme, ExtensionThemeColors.selected("hub-A", ExtensionThemeColors.selection("hub-A", theme), listOf(theme)))
        assertNull(ExtensionThemeColors.selected("hub-A", ExtensionThemeColors.selection("hub-A", theme), emptyList()))
        for (style in AppStyle.entries) for (dark in listOf(false, true)) {
            val base = Palettes.of(style, dark)
            val next = ExtensionThemeColors.palette(theme, style, dark)
            assertEquals(base.background, next.background)
            assertEquals(base.ink, next.ink)
            assertEquals(base.surface, next.surface)
            assertEquals(base.working, next.working)
            assertEquals(base.done, next.done)
            assertEquals(base.danger, next.danger)
            assertEquals(base.positive, next.positive)
            assertEquals(base.caution, next.caution)
            assertEquals(base.ai, next.ai)
            assertNotEquals(base.accent, next.accent)
        }
    }

    @Test fun optionalRolesHaveReadableTextAndButtonGlyphs() {
        val roleTheme = theme.copy(light = theme.light + mapOf("danger" to "#990022", "positive" to "#006633", "caution" to "#704400", "ai" to "#4A3480"),
            dark = theme.dark + mapOf("danger" to "#FFBBBB", "positive" to "#AADDCC", "caution" to "#EEDDBB", "ai" to "#CCBBFF"))
        assertEquals(emptyList<String>(), ExtensionThemeColors.problems(roleTheme))
        for (style in AppStyle.entries) for (dark in listOf(false, true)) {
            val p = ExtensionThemeColors.palette(roleTheme, style, dark)
            assertTrue(ExtensionThemeColors.contrast(p.danger.onDot, p.danger.dot) >= 4.5)
            assertTrue(ExtensionThemeColors.contrast(p.tone(Tone.Positive).text, p.surfaceOpaque) >= 4.5)
            assertTrue(ExtensionThemeColors.contrast(p.ai, p.surfaceOpaque) >= 4.5)
        }
        assertEquals(Color.White, ExtensionThemeColors.on(Color.Black))
        assertEquals(Color.Black, ExtensionThemeColors.on(Color.White))
    }
}
