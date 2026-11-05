package persona.feature.chat

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import java.time.ZoneOffset
import java.util.Locale

/** The time under your message follows the app's language, never a fixed `HH:mm` (#4043). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class UserBubbleTimeTest {
    private val zone = ZoneOffset.UTC

    @Test fun timeUsesTheLanguagesClock() {
        assertEquals("14:30", userBubbleTime("2026-11-05T14:30:00Z", Locale.SIMPLIFIED_CHINESE, zone))
        assertEquals("14:30", userBubbleTime("2026-11-05T14:30:00Z", Locale.JAPANESE, zone))
        val en = userBubbleTime("2026-11-05T22:30:00+08:00", Locale.US, zone).orEmpty()
        assertTrue(en, en.startsWith("2:30") && en.endsWith("PM"))
    }

    @Test fun notATimeShowsNothing() {
        assertNull(userBubbleTime("", Locale.US, zone))
        assertNull(userBubbleTime("nope", Locale.US, zone))
    }
}
