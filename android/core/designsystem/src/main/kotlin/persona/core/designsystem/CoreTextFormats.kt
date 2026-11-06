package persona.core.designsystem

import android.content.res.Resources
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.platform.LocalResources
import persona.core.contracts.CoreText
import persona.core.domain.tasks.DayLabel
import java.time.ZoneId

/** A day divider's words: "Today · Tue, Nov 3", "Yesterday · Tue, Nov 2", the date with its year when not this year. */
fun DayLabel.resolve(resources: Resources): String {
    val locale = resources.configuration.locales[0]
    val skeleton = if (kind == DayLabel.Kind.OtherYear) "yMMMdEEE" else "MMMdEEE"
    val day = LocalizedFormats.format(date.atStartOfDay(ZoneId.systemDefault()).toInstant(), skeleton, locale)
    return when (kind) {
        DayLabel.Kind.Today -> CoreText.of(CoreText.Key.DayToday, day).resolve(resources)
        DayLabel.Kind.Yesterday -> CoreText.of(CoreText.Key.DayYesterday, day).resolve(resources)
        DayLabel.Kind.ThisYear, DayLabel.Kind.OtherYear -> day
    }
}

/** [resolve] in the page's language. */
@Composable
@ReadOnlyComposable
fun DayLabel.text(): String = resolve(LocalResources.current)
