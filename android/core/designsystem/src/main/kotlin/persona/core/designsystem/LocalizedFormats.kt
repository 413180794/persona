package persona.core.designsystem

import android.content.Context
import android.text.format.DateFormat
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.platform.LocalConfiguration
import java.text.NumberFormat
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

/**
 * Dates, times and numbers in the language the text is in (docs/i18n.md). Formats come from skeletons (which fields
 * to show) and the locale picks order and words: `MMMd` is 「11月5日」, "Nov 5" and 「11月5日」. Never write a
 * pattern with words in it (`M月d日`) in a page.
 */
object LocalizedFormats {
    /** A month and day: 11月5日 · Nov 5. */
    const val MONTH_DAY = "MMMd"
    /** A month, day and time: 11月5日 14:30 · Nov 5, 2:30 PM. */
    const val MONTH_DAY_TIME = "MMMdjmm"
    /** A full date: 2026年11月5日 · Nov 5, 2026. */
    const val DATE = "yMMMd"
    /** A full date and time. */
    const val DATE_TIME = "yMMMdjmm"
    /** A time in the phone's 12 / 24 hour habit for the language: 14:30 · 2:30 PM. */
    const val TIME = "jmm"
    /** A time with seconds. */
    const val TIME_SECONDS = "jmmss"

    /** A formatter for [skeleton] in [locale] and [zone]. */
    fun formatter(skeleton: String, locale: Locale, zone: ZoneId = ZoneId.systemDefault()): DateTimeFormatter =
        DateTimeFormatter.ofPattern(DateFormat.getBestDateTimePattern(locale, skeleton), locale).withZone(zone)

    fun format(instant: Instant, skeleton: String, locale: Locale, zone: ZoneId = ZoneId.systemDefault()): String =
        formatter(skeleton, locale, zone).format(instant)

    /** A count with the language's grouping: 12,345 · 12,345 · 12,345. */
    fun number(value: Number, locale: Locale): String = NumberFormat.getInstance(locale).format(value)

    /** A share, from 0 to 1: 42%. */
    fun percent(fraction: Double, locale: Locale): String = NumberFormat.getPercentInstance(locale).format(fraction)

    /** The locale the text of [context] is in, for code outside composition (notifications, view models). */
    fun locale(context: Context): Locale = context.resources.configuration.locales[0]
}

/** The locale the page's text is in; dates and numbers on it use this. */
val currentLocale: Locale
    @Composable @ReadOnlyComposable get() = LocalConfiguration.current.locales[0]
