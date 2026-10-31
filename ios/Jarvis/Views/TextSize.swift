import SwiftUI
import UIKit

/// The app's own text size (设置 › 界面风格 › 字号: 小 / 标准 / 大), on top of the system's. It moves the Dynamic Type
/// size one step down or up from whatever the system is set to, so every `Theme.Font` level, every `@ScaledMetric`
/// and BeautifulUI's sizes (`ScaledMetric` too) move together and keep their ratios; changing the system's text size
/// still applies underneath. At either end of the range it stays where it is.
enum TextSize: String, CaseIterable, Identifiable {
    case small, standard, large

    var id: String { rawValue }

    var title: String {
        switch self {
        case .small: "小"
        case .standard: "标准"
        case .large: "大"
        }
    }

    private var step: Int {
        switch self {
        case .small: -1
        case .standard: 0
        case .large: 1
        }
    }

    func applied(to system: DynamicTypeSize) -> DynamicTypeSize {
        let all = DynamicTypeSize.allCases
        guard let i = all.firstIndex(of: system) else { return system }
        return all[min(max(i + step, 0), all.count - 1)]
    }

    /// `ui.textSize` (also a launch argument: `-ui.textSize large`).
    static var current: TextSize { TextSize(rawValue: UserDefaults.standard.string(forKey: "ui.textSize") ?? "") ?? .standard }
}

extension Theme {
    /// How much larger body text is at this Dynamic Type size than at the default: for text drawn at a point size of
    /// its own (Lark messages and cards keep Feishu's sizes), so it follows the system and 小 / 标准 / 大 too.
    static func textScale(_ size: DynamicTypeSize) -> CGFloat {
        if let k = textScales[size] { return k }
        let traits = UITraitCollection(preferredContentSizeCategory: UIContentSizeCategory(size))
        let k = UIFontMetrics(forTextStyle: .body).scaledValue(for: 17, compatibleWith: traits) / 17
        textScales[size] = k
        return k
    }

    /// Rows in long lists ask for every redraw; there are twelve sizes.
    private static var textScales: [DynamicTypeSize: CGFloat] = [:]
}

extension View {
    /// The level's line height (`Theme.Level`) for text that wraps: messages, previews, descriptions. It scales with
    /// the text size like the font does.
    func lineHeight(_ level: Theme.Level) -> some View { modifier(LineHeight(level: level)) }
}

private struct LineHeight: ViewModifier {
    let level: Theme.Level
    @Environment(\.dynamicTypeSize) private var typeSize

    func body(content: Content) -> some View {
        content.lineSpacing(level.extraLeading * Theme.textScale(typeSize))
    }
}
