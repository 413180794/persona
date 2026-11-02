package jarvis.android.ui.design

import androidx.compose.material3.Typography
import androidx.compose.runtime.Immutable
import androidx.compose.ui.text.PlatformTextStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.LineHeightStyle
import androidx.compose.ui.unit.em
import androidx.compose.ui.unit.sp

/**
 * The type scale (iOS `Theme.Font`): every text in the app is one of these levels. As in iOS, levels differ more by
 * weight and color than by size: a row's title is the body's size in semibold, a section header the caption's size in
 * semibold secondary. Sizes in sp follow the system's font scale. Each level carries its line height (iOS
 * `Theme.Level.lineHeight`) between lines; the first line's top and the last line's bottom are trimmed, so a
 * single line is as tall as its letters.
 */
@Immutable
data class JTypeScale(
    /** The home page's greeting, the drawer's name (28 bold). */
    val display: TextStyle,
    /** A detail page's or a sheet's title (22 semibold). */
    val title: TextStyle,
    /** Titles of rows, cards, panels; Markdown headings below the first (17 semibold). */
    val heading: TextStyle,
    /** Messages, replies, mail (17). */
    val body: TextStyle,
    /** Previews, subtitles, descriptions in cards (15). */
    val secondary: TextStyle,
    /** Times, places, states, meta lines (13). */
    val caption: TextStyle,
    /** A list's section header: the caption's size, semibold, in the secondary color (13). */
    val sectionHeader: TextStyle,
    /** Chips, badges, counts, small labels on controls (12). */
    val label: TextStyle,
    /** Code, diffs, paths (13, monospaced). */
    val code: TextStyle,
) {
    // Emphasis inside a level: pages use these, never a fontWeight of their own.
    val bodyStrong get() = body.copy(fontWeight = FontWeight.SemiBold)
    val bodyHeavy get() = body.copy(fontWeight = FontWeight.Bold)
    val headingMedium get() = heading.copy(fontWeight = FontWeight.Medium)
    val secondaryStrong get() = secondary.copy(fontWeight = FontWeight.SemiBold)
    val secondaryMedium get() = secondary.copy(fontWeight = FontWeight.Medium)
    val captionStrong get() = caption.copy(fontWeight = FontWeight.SemiBold)
    val captionMedium get() = caption.copy(fontWeight = FontWeight.Medium)
    val captionHeavy get() = caption.copy(fontWeight = FontWeight.Bold)
    val labelStrong get() = label.copy(fontWeight = FontWeight.SemiBold)
    val labelMedium get() = label.copy(fontWeight = FontWeight.Medium)
    val labelHeavy get() = label.copy(fontWeight = FontWeight.Bold)
    /** Hashes and paths in a label's size (iOS `Theme.Font.label.monospaced()`). */
    val labelMono get() = label.copy(fontFamily = FontFamily.Monospace)
    /** A Git status letter (iOS `Theme.Font.labelHeavy.monospaced()`). */
    val labelMonoHeavy get() = labelHeavy.copy(fontFamily = FontFamily.Monospace)

    /** The same scale as Material's slots, so Material's own components (fields, menus, dialogs) read it too. */
    fun material(): Typography = Typography(
        displayLarge = display, displayMedium = display, displaySmall = display,
        headlineLarge = title, headlineMedium = title, headlineSmall = title,
        titleLarge = title, titleMedium = heading, titleSmall = secondaryStrong,
        bodyLarge = body, bodyMedium = secondary, bodySmall = caption,
        labelLarge = secondaryStrong, labelMedium = labelMedium, labelSmall = label,
    )

    companion object {
        private val trimmed = LineHeightStyle(alignment = LineHeightStyle.Alignment.Center, trim = LineHeightStyle.Trim.Both)

        private fun level(family: FontFamily?, size: Float, weight: FontWeight, lineHeight: Float) = TextStyle(
            fontFamily = family,
            fontSize = size.sp,
            fontWeight = weight,
            lineHeight = lineHeight.em,
            lineHeightStyle = trimmed,
            platformStyle = PlatformTextStyle(includeFontPadding = false),
        )

        /** [family] is the style's face (黏土's rounded one), null for the system's. */
        fun of(family: FontFamily?): JTypeScale = JTypeScale(
            display = level(family, 28f, FontWeight.Bold, 1.2f),
            title = level(family, 22f, FontWeight.SemiBold, 1.25f),
            heading = level(family, 17f, FontWeight.SemiBold, 1.3f),
            body = level(family, 17f, FontWeight.Normal, 1.45f),
            secondary = level(family, 15f, FontWeight.Normal, 1.4f),
            caption = level(family, 13f, FontWeight.Normal, 1.35f),
            sectionHeader = level(family, 13f, FontWeight.SemiBold, 1.35f),
            label = level(family, 12f, FontWeight.Normal, 1.3f),
            code = level(FontFamily.Monospace, 13f, FontWeight.Normal, 1.35f),
        )
    }
}
