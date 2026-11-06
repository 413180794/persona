import BeautifulUI
import SwiftUI

// BeautifulUI (apple/Packages/BeautifulUI) reads its colors, radii and type size from `BUITheme` in the environment.
// RootView puts in the one built here from the current spec (style × Theme Color), so its components follow them like the
// rest of the app. The library's own palette is never used.

extension BUITheme {
    static func persona(_ spec: StyleSpec = .current) -> BUITheme {
        // Secondary text: tertiaryLabel is 1.9:1 on white, under the 3:1 the visual pass brought every word to, so the
        // library's third ink is secondaryLabel too (3.4:1 light, 6.4:1 dark).
        let secondary = Color(uiColor: .secondaryLabel)
        let colors = BUIColors(
            page: spec.page,
            canvas: spec.page,
            surface: spec.card,
            inset: Theme.Fill.well,
            hover: Theme.Fill.well,
            hover2: Theme.Fill.pressed,
            ink: Color(uiColor: .label),
            ink2: secondary,
            ink3: secondary,
            line: spec.cardStroke,
            lineStrong: Color.primary.opacity(0.16),
            lineSoft: Color.primary.opacity(0.05),
            field: Theme.Fill.chip,
            accent: spec.accent,
            // Keywords in code: a blue with text contrast on the code background in every style (the accents of
            // Claude / Island are under 4.5:1 there).
            accentInk: Theme.Hue.blue,
            accentTint: spec.accent.opacity(0.12),
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
        // The library's sizes are the web original's (13 pt labels); ×1.2 puts its labels at Persona' tool rows (15).
        // Entrances off: the chat list rebuilds rows as they scroll in, and an opened history would fade up at once.
        return BUITheme(colors: colors, radii: radii, typeScale: 1.2, entrances: false, strings: .persona)
    }

    /// For code and diffs: on the code background, at the code size Persona already uses (12–13).
    var onCode: BUITheme {
        var t = self
        t.colors.surface = Theme.codeBackground
        t.typeScale = 1
        return t
    }
}

extension BUIStrings {
    /// What the components show and VoiceOver says. The open state uses the Agents list's words.
    static let persona: BUIStrings = {
        var w = BUIStrings(expanded: String(localized: "Expanded"), collapsed: String(localized: "Collapsed [Readiness Card ee6046]"))
        w.prompt = String(localized: "Input Field")
        w.listening = String(localized: "Listening…")
        w.attach = String(localized: "Add [Beautiful U I Bridge 7a8a11]") // the + of the old composer read so too
        w.mention = String(localized: "Mention a file or session")
        w.commands = String(localized: "Commands [Beautiful U I Bridge 6ee4a9]")
        w.chooseModel = String(localized: "Choose Model")
        w.startDictation = String(localized: "Dictate")
        w.stopDictation = String(localized: "Stop Dictation")
        w.send = String(localized: "Send")
        w.stop = String(localized: "Interrupt [Composer Buttons f9d193]")
        w.connect = String(localized: "Connect [V N C Page a55741]")
        w.connected = String(localized: "Connected [V N C Page 5be032]")
        w.sourcesHint = String(localized: "Keep typing to filter")
        w.commandsHint = String(localized: "Picking one fills it in; it's sent only when you tap Send")
        w.noMatches = String(localized: "No “%@”")
        w.remove = String(localized: "Remove %@ [Beautiful U I Bridge 271f20]")
        return w
    }()
}
