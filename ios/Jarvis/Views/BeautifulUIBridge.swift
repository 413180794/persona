import BeautifulUI
import SwiftUI

// BeautifulUI (ios/Packages/BeautifulUI) reads its colors, radii and type size from `BUITheme` in the environment.
// RootView puts in the one built here from the current style, so its components follow the five styles like the rest
// of the app. The library's own palette is never used.

extension BUITheme {
    static func jarvis(_ style: AppStyle) -> BUITheme {
        // Secondary text: tertiaryLabel is 1.9:1 on white, under the 3:1 the visual pass brought every word to, so the
        // library's third ink is secondaryLabel too (3.4:1 light, 6.4:1 dark).
        let secondary = Color(uiColor: .secondaryLabel)
        let colors = BUIColors(
            page: Theme.pageBackground(style),
            canvas: Theme.pageBackground(style),
            surface: Theme.card(style),
            inset: Theme.Fill.well,
            hover: Theme.Fill.well,
            hover2: Theme.Fill.pressed,
            ink: Color(uiColor: .label),
            ink2: secondary,
            ink3: secondary,
            line: Theme.cardStroke,
            lineStrong: Color.primary.opacity(0.16),
            lineSoft: Color.primary.opacity(0.05),
            field: Theme.Fill.chip,
            accent: Theme.accent(style),
            // Keywords in code: a blue with text contrast on the code background in every style (the accents of
            // Claude / Island are under 4.5:1 there).
            accentInk: Theme.Hue.blue,
            accentTint: Theme.accent(style).opacity(0.12),
            green: Theme.Diff.add,
            greenTint: Theme.Diff.addFill,
            orange: Theme.cautionText,
            orangeTint: Theme.caution.opacity(0.14),
            red: Theme.Diff.del,
            redTint: Theme.Diff.delFill,
            tooltipBackground: BUIColors.standard.tooltipBackground,
            tooltipForeground: BUIColors.standard.tooltipForeground,
            tooltipMuted: BUIColors.standard.tooltipMuted,
            tooltipBorder: BUIColors.standard.tooltipBorder,
            stripe: BUIColors.standard.stripe,
            stripeBackground: BUIColors.standard.stripeBackground
        )
        let radii = BUIRadii(small: 4, chip: Theme.Radius.badge, control: Theme.Radius.control, card: Theme.Radius.card,
                             window: Theme.Radius.panel)
        // The library's sizes are the web original's (13 pt labels); ×1.2 puts its labels at Jarvis' tool rows (15).
        // Entrances off: the chat list rebuilds rows as they scroll in, and an opened history would fade up at once.
        return BUITheme(colors: colors, radii: radii, typeScale: 1.2, entrances: false)
    }

    /// For code and diffs: on the code background, at the code size Jarvis already uses (12–13).
    var onCode: BUITheme {
        var t = self
        t.colors.surface = Theme.codeBackground
        t.typeScale = 1
        return t
    }
}
