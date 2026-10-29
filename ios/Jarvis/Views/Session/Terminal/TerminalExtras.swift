import SwiftUI

// The terminal tab's operations layer: saved shortcuts, the key bar's buttons, appearance, and suggestions taken from
// what the pane shows (never the computer's shell history).

/// Settings kept on the phone (`@AppStorage`).
enum TerminalPrefs {
    static let shortcuts = "terminalShortcuts" // JSON [String]
    static let keys = "terminalKeys" // the optional key bar buttons shown, comma separated
    static let autoHide = "terminalAutoHideKeys"
    static let fontSize = "terminalFontSize"
    static let font = "terminalFont"
    static let theme = "terminalTheme"
    static let wrap = "terminalWrap"
    /// "direct": the keyboard types into the terminal; "field": a text field under it sends what is written.
    static let inputMode = "terminalInputMode"
    static let tipsSeen = "terminalTipsSeen"
    /// "phone": the phone drives the terminal at its own size while the tab is open; "mirror": it shows the pane at the
    /// computer's size and folds long rows.
    static let size = "terminalSize"
    static let defaultFontSize = 13.0
    static let defaultShortcuts = ["git status", "git diff --stat", "ls -la", "/compact", "/status"]
    static let allKeys: [(id: String, label: String)] = [("esc", "esc"), ("tab", "tab"), ("ctrlc", "⌃C"), ("shortcuts", "快捷命令"),
                                                         ("paste", "粘贴"), ("alt", "alt"), ("shift", "⇧ shift"), ("pad", "方向键")]
    /// alt and shift are a tap away in the settings (Moshi shows only ctrl too): the rest fits a phone's width.
    static let defaultKeys = allKeys.map(\.id).filter { $0 != "alt" && $0 != "shift" }.joined(separator: ",")

    static func decode(_ json: String) -> [String] {
        guard !json.isEmpty, let list = try? JSONDecoder().decode([String].self, from: Data(json.utf8)) else { return defaultShortcuts }
        return list
    }

    static func encode(_ list: [String]) -> String {
        (try? String(decoding: JSONEncoder().encode(list), as: UTF8.self)) ?? ""
    }
}

/// 设置 › 终端: how the terminal looks (it is only for looking: no keys, no typing) and what the gestures do.
struct TerminalSettingsView: View {
    @AppStorage(TerminalPrefs.wrap) private var wrap = true
    @AppStorage(TerminalPrefs.fontSize) private var fontSize = TerminalPrefs.defaultFontSize
    @AppStorage(TerminalPrefs.font) private var fontID = TerminalFont.jetbrains.rawValue
    @AppStorage(TerminalPrefs.theme) private var themeID = TerminalTheme.defaultID

    var body: some View {
        Form {
            Section {
                TerminalPreview(fontSize: fontSize, font: TerminalFont(rawValue: fontID) ?? .jetbrains,
                                theme: .named(themeID), wrap: wrap)
                    .frame(height: TerminalPreview.height(fontSize))
                    .listRowInsets(EdgeInsets())
                ForEach(TerminalTheme.all, id: \.id) { t in
                    Button { themeID = t.id } label: { ThemeRow(theme: t, on: t.id == themeID) }
                        .buttonStyle(.plain)
                }
            } header: {
                Text("配色")
            } footer: {
                Text("墨黑、深夜不随系统变化；Jarvis、Solarized 跟着系统的浅色 / 深色。")
            }
            Section {
                Picker("字体", selection: $fontID) {
                    ForEach(TerminalFont.allCases, id: \.rawValue) { Text($0.name).tag($0.rawValue) }
                }
                Stepper(value: $fontSize, in: 7...20, step: 0.5) {
                    Text("字号 \(fontSize, specifier: "%.1f")").monospacedDigit()
                }
                Toggle("长行按手机宽度换行", isOn: $wrap)
            } header: {
                Text("文字")
            } footer: {
                Text("终端按电脑上窗格的尺寸显示，不会改动电脑。关掉换行时长行左右滑着看。在终端上双指捏合也能调字号。")
            }
            Section("手势") {
                Label("上滑：一直往前翻历史", systemImage: "arrow.up")
                Label("捏合调字号", systemImage: "arrow.up.left.and.arrow.down.right")
                Label("双指左右滑：切换同一空间里的 Agent", systemImage: "arrow.left.arrow.right")
                Label("双指上下滑：切换到相邻的空间", systemImage: "arrow.up.arrow.down")
                Label("长按：选择、拷贝", systemImage: "text.cursor")
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
        }
        .navigationTitle("终端")
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// A few lines of a Claude session in the chosen look, for the settings.
struct TerminalPreview: View {
    var fontSize: Double
    var font: TerminalFont
    var theme: TerminalTheme
    var wrap: Bool

    private static let sample: TerminalContent = {
        let ansi = "\u{1B}[38;2;215;119;87m⏺\u{1B}[0m 好的，先跑一下测试：\r\n"
            + "  ⎿  \u{1B}[32mok\u{1B}[0m  ./screen  \u{1B}[2m0.41s\u{1B}[0m\r\n"
            + "\u{1B}[38;2;136;136;136m" + String(repeating: "─", count: 60) + "\u{1B}[0m\r\n"
            + "❯ \u{1B}[7m \u{1B}[0m\r\n"
            + "\u{1B}[38;2;136;136;136m" + String(repeating: "─", count: 60) + "\u{1B}[0m\r\n"
            + "  \u{1B}[35m⏵⏵ auto mode on\u{1B}[0m \u{1B}[2m(shift+tab to cycle)\u{1B}[0m"
        return TerminalContent(rows: TerminalGrid.rows(fromANSI: ansi), cols: 60, cursor: nil)
    }()

    /// Room for its six rows at a size.
    static func height(_ size: Double) -> CGFloat { min(280, 6 * ceil(size * 1.42) + 22) }

    var body: some View {
        TerminalView(content: Self.sample, wrap: wrap, fontSize: fontSize, font: font, theme: theme)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

/// A theme in the list: its name and its colors.
struct ThemeRow: View {
    @Environment(\.colorScheme) private var scheme
    var theme: TerminalTheme
    var on: Bool

    var body: some View {
        let side = theme.side(scheme == .dark)
        HStack(spacing: 10) {
            Text(theme.name).lineLimit(1)
            Spacer(minLength: 8)
            HStack(spacing: 4) {
                ForEach([side.background, side.foreground, side.ansi[1], side.ansi[2], side.ansi[4]], id: \.self) { c in
                    Circle().fill(Color(hex: c)).frame(width: 14, height: 14)
                        .overlay(Circle().stroke(Color.primary.opacity(0.15), lineWidth: 0.5))
                }
            }
            Image(systemName: "checkmark").font(.body.weight(.semibold)).foregroundStyle(Theme.accent).opacity(on ? 1 : 0)
        }
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
    }
}
