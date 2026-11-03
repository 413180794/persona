import BeautifulUI
import SwiftUI

// BeautifulUI (apple/Packages/BeautifulUI) reads its colors, radii and type size from `BUITheme` in the environment.
// RootView puts in the one built here from the current spec (style × 主题色), so its components follow them like the
// rest of the app. The library's own palette is never used.

extension BUITheme {
    static func jarvis(_ spec: StyleSpec = .current) -> BUITheme {
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
        return BUITheme(colors: colors, radii: radii, typeScale: 1.2, entrances: false, strings: .jarvis)
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
    static let jarvis: BUIStrings = {
        var w = BUIStrings(expanded: "已展开", collapsed: "已折叠")
        w.prompt = "输入框"
        w.listening = "正在听…"
        w.attach = "添加" // the + of the old composer read so too
        w.mention = "提及文件或会话"
        w.commands = "指令"
        w.chooseModel = "选择模型"
        w.startDictation = "听写"
        w.stopDictation = "停止听写"
        w.send = "发送"
        w.stop = "中断"
        w.connect = "连接"
        w.connected = "已连接"
        w.sourcesHint = "接着输入可以筛选"
        w.commandsHint = "选中后会填进输入框，点发送才发出"
        w.noMatches = "没有「%@」"
        w.remove = "移除 %@"
        return w
    }()
}
