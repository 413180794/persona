package persona.feature.settings

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset
import java.util.Locale

/** Diagnostics, pairing and Messages times follow the app's language, never a fixed `MM-dd HH:mm:ss` (#4043). */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class SettingsTimeLanguagesTest {
    private val zone = ZoneOffset.UTC
    private val at = "2026-11-05T14:30:05Z"
    private val sameDay = LocalDate.of(2026, 11, 5)
    private val nextDay = LocalDate.of(2026, 11, 6)

    @Test fun diagnosticsTimesTodayAreTheLanguagesClockWithSeconds() {
        assertEquals("14:30:05", localTime(at, Locale.SIMPLIFIED_CHINESE, zone, sameDay))
        assertEquals("14:30:05", localTime(at, Locale.JAPANESE, zone, sameDay))
        val en = localTime(at, Locale.US, zone, sameDay).orEmpty()
        assertTrue(en, en.startsWith("2:30:05") && en.endsWith("PM"))
    }

    @Test fun diagnosticsTimesBeforeTodayCarryTheDateInTheLanguage() {
        val zh = localTime(at, Locale.SIMPLIFIED_CHINESE, zone, nextDay).orEmpty()
        assertTrue(zh, zh.startsWith("11月5日") && zh.endsWith("14:30:05"))
        val ja = localTime(at, Locale.JAPANESE, zone, nextDay).orEmpty()
        assertTrue(ja, ja.startsWith("11月5日") && ja.endsWith("14:30:05"))
        val en = localTime(at, Locale.US, zone, nextDay).orEmpty()
        assertTrue(en, en.startsWith("Nov 5") && en.contains("2:30:05") && en.contains("PM"))
        assertTrue(listOf(zh, ja, en).none { it.contains("11-05") })
    }

    @Test fun pairingStepTimesUseTheLanguagesClock() {
        val instant = Instant.parse(at)
        assertEquals("14:30:05", pairingStepTime(instant, Locale.SIMPLIFIED_CHINESE, zone))
        assertEquals("14:30:05", pairingStepTime(instant, Locale.JAPANESE, zone))
        val en = pairingStepTime(instant, Locale.US, zone)
        assertTrue(en, en.startsWith("2:30:05") && en.endsWith("PM"))
    }

    @Test fun messagesRowShowsTheTimeTodayAndTheDateBefore() {
        assertEquals("14:30", messagesShortTime(at, Locale.SIMPLIFIED_CHINESE, zone, sameDay))
        assertEquals("14:30", messagesShortTime(at, Locale.JAPANESE, zone, sameDay))
        val enToday = messagesShortTime(at, Locale.US, zone, sameDay).orEmpty()
        assertTrue(enToday, enToday.startsWith("2:30") && enToday.endsWith("PM"))
        assertEquals("11月5日", messagesShortTime(at, Locale.SIMPLIFIED_CHINESE, zone, nextDay))
        assertEquals("11月5日", messagesShortTime(at, Locale.JAPANESE, zone, nextDay))
        assertEquals("Nov 5", messagesShortTime(at, Locale.US, zone, nextDay))
        val nextYear = LocalDate.of(2027, 1, 2)
        assertEquals("2026年11月5日", messagesShortTime(at, Locale.SIMPLIFIED_CHINESE, zone, nextYear))
        assertEquals("2026年11月5日", messagesShortTime(at, Locale.JAPANESE, zone, nextYear))
        assertEquals("Nov 5, 2026", messagesShortTime(at, Locale.US, zone, nextYear))
        assertNull(messagesShortTime("not a time", Locale.US, zone, sameDay))
    }

    @Test fun messagesSeparatorBeforeYesterdayIsDateAndTimeInTheLanguage() {
        val instant = Instant.parse(at)
        val zh = messagesDayTime(instant, Locale.SIMPLIFIED_CHINESE, zone)
        assertTrue(zh, zh.startsWith("2026年11月5日") && zh.endsWith("14:30"))
        val ja = messagesDayTime(instant, Locale.JAPANESE, zone)
        assertTrue(ja, ja.startsWith("2026年11月5日") && ja.endsWith("14:30"))
        val en = messagesDayTime(instant, Locale.US, zone)
        assertTrue(en, en.startsWith("Nov 5, 2026") && en.contains("2:30") && en.endsWith("PM"))
    }
}
