package persona.core.designsystem

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

/**
 * A window other than the main one (share, the capsule, the approve dialog) draws with [PersonaTheme]'s defaults:
 * Light/dark and theme color reach it through [AppAppearance] (#3048 review), on a phone left in light mode.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "notnight")
class AppAppearanceTest {
    @get:Rule val rule = createComposeRule()

    @After fun reset() {
        AppAppearance.scheme = ColorSchemeChoice.Default
        AppAppearance.accent = null
    }

    @Test fun aSecondaryHostFollowsTheChosenSchemeAndAccent() {
        AppAppearance.scheme = ColorSchemeChoice.Dark
        AppAppearance.accent = ThemeColor.Coral.preset
        var dark = false
        var accent = Color.Unspecified
        var bubbleText: Color? = null
        rule.setContent {
            PersonaTheme(style = AppStyle.Glass) {
                dark = PersonaTheme.darkTheme
                accent = PersonaTheme.palette.accent
                bubbleText = PersonaTheme.palette.userBubbleText
            }
        }
        rule.waitForIdle()
        assertTrue(dark)
        assertEquals(ThemeColors.palette(Palettes.of(AppStyle.Glass, true), ThemeColor.Coral.preset!!).accent, accent)
        assertEquals(Color.White, bubbleText)
    }

    @Test fun followingTheSystemIsTheSystems() {
        var dark = true
        rule.setContent { PersonaTheme { dark = PersonaTheme.darkTheme } }
        rule.waitForIdle()
        assertEquals(false, dark)
    }
}
