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

/// Saved commands over the key bar: a tap puts one in the text field, a double tap sends it with return.
struct ShortcutsPanel: View {
    var items: [String]
    var hint = "快捷 · 单击填入，双击发送"
    var fill: (String) -> Void
    var send: (String) -> Void
    var edit: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Text(hint).font(.caption.weight(.semibold)).foregroundStyle(.secondary).lineLimit(1)
                Spacer(minLength: 4)
                Button("编辑", action: edit).font(.caption.weight(.semibold)).lineLimit(1)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            if items.isEmpty {
                Text("还没有快捷命令，点「编辑」添加").font(.footnote).foregroundStyle(.secondary)
                    .padding(.horizontal, 14).padding(.bottom, 12)
            }
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                        Text(item)
                            .font(.system(.subheadline, design: .monospaced))
                            .lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 14)
                            .padding(.vertical, 10)
                            .contentShape(.rect)
                            .onTapGesture(count: 2) { Haptics.success(); send(item) }
                            .onTapGesture { Haptics.tap(); fill(item) }
                            .accessibilityAddTraits(.isButton)
                            .accessibilityHint("单击填入输入框，双击直接发送")
                            .accessibilityAction(named: "直接发送") { send(item) }
                        Divider().padding(.leading, 14)
                    }
                }
            }
            .frame(maxHeight: 230)
            .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: 360)
        .surface(in: .rect(cornerRadius: 22, style: .continuous))
        .shadow(color: .black.opacity(0.18), radius: 18, y: 6)
    }
}

/// 终端设置: shortcuts, the key bar, appearance, and what the gestures do.
struct TerminalSettingsView: View {
    @Environment(\.dismiss) private var dismiss
    @AppStorage(TerminalPrefs.shortcuts) private var shortcutsJSON = ""
    @AppStorage(TerminalPrefs.keys) private var keys = TerminalPrefs.defaultKeys
    @AppStorage(TerminalPrefs.autoHide) private var autoHide = false
    @AppStorage(TerminalPrefs.wrap) private var wrap = true
    @AppStorage(TerminalPrefs.fontSize) private var fontSize = TerminalPrefs.defaultFontSize
    @AppStorage(TerminalPrefs.font) private var fontID = TerminalFont.jetbrains.rawValue
    @AppStorage(TerminalPrefs.theme) private var themeID = TerminalTheme.defaultID
    @AppStorage(TerminalPrefs.inputMode) private var inputMode = "direct"
    @AppStorage(TerminalPrefs.size) private var size = "phone"
    @State private var newShortcut = ""

    private var shortcuts: Binding<[String]> {
        Binding(get: { TerminalPrefs.decode(shortcutsJSON) }, set: { shortcutsJSON = TerminalPrefs.encode($0) })
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    Picker("终端尺寸", selection: $size) {
                        Text("手机尺寸").tag("phone")
                        Text("电脑尺寸").tag("mirror")
                    }
                    .pickerStyle(.segmented)
                } header: {
                    Text("尺寸")
                } footer: {
                    Text(size == "phone" ? "打开终端时由手机接管：电脑上的窗格变成手机的行列数，Claude 按手机宽度排版；离开终端、App 进后台或断线后，窗格回到电脑上的尺寸。"
                         : "不改电脑上的窗格，按电脑的宽度显示，长行在手机上折行（或左右滑）。")
                }
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
                    Text("在终端上双指捏合也能调字号。中文用系统字体，每个字占两格。")
                }
                Section {
                    Picker("输入方式", selection: $inputMode) {
                        Text("直接打字").tag("direct")
                        Text("输入框").tag("field")
                    }
                    .pickerStyle(.segmented)
                } header: {
                    Text("输入")
                } footer: {
                    Text(inputMode == "field" ? "在终端下方的输入框里写好，按回车一次发出去；键栏的 ctrl / alt / ⇧ 对输入框里打的下一个字也生效。"
                         : "点终端弹出键盘，每个键直接发到终端（像在电脑上一样，输入法、听写都能用）；键栏贴在键盘上方。")
                }
                Section {
                    ForEach(Array(shortcuts.wrappedValue.enumerated()), id: \.offset) { _, s in
                        Text(s).font(.system(.body, design: .monospaced)).lineLimit(1)
                    }
                    .onDelete { shortcuts.wrappedValue.remove(atOffsets: $0) }
                    .onMove { shortcuts.wrappedValue.move(fromOffsets: $0, toOffset: $1) }
                    HStack {
                        TextField("新的快捷命令", text: $newShortcut)
                            .font(.system(.body, design: .monospaced))
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .onSubmit(add)
                        Button("添加", action: add).lineLimit(1).disabled(newShortcut.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                } header: {
                    Text("快捷命令")
                } footer: {
                    Text("在键栏的闪电里：单击输入，双击带回车执行。左上角「编辑」可以删除、排序。")
                }
                Section {
                    Toggle(isOn: .constant(true)) { Text("ctrl") }.disabled(true)
                    ForEach(TerminalPrefs.allKeys, id: \.id) { k in
                        Toggle(k.label, isOn: keyBinding(k.id))
                    }
                    Toggle("不打字时收起键栏", isOn: $autoHide)
                } header: {
                    Text("键栏")
                } footer: {
                    Text("ctrl 一直在，键盘按钮和方向键在最右。放不下时键栏可以左右滑。")
                }
                Section("手势") {
                    Label("点终端：弹出键盘直接打字", systemImage: "keyboard")
                    Label("捏合调字号", systemImage: "arrow.up.left.and.arrow.down.right")
                    Label("双指左右滑：切换同一空间里的 Agent", systemImage: "arrow.left.arrow.right")
                    Label("双指上下滑：切换到相邻的空间", systemImage: "arrow.up.arrow.down")
                    Label("双击终端：粘贴剪贴板", systemImage: "doc.on.clipboard")
                    Label("长按：选择、拷贝", systemImage: "text.cursor")
                    Label("听写：键盘上的麦克风", systemImage: "mic")
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
            }
            .navigationTitle("终端设置")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) { EditButton() }
                ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } }
            }
        }
    }

    private func add() {
        let s = newShortcut.trimmingCharacters(in: .whitespaces)
        guard !s.isEmpty else { return }
        shortcuts.wrappedValue.append(s)
        newShortcut = ""
    }

    private func keyBinding(_ id: String) -> Binding<Bool> {
        Binding(get: { keys.split(separator: ",").contains(Substring(id)) }, set: { on in
            var set = keys.split(separator: ",").map(String.init).filter { $0 != id }
            if on { set.append(id) }
            keys = TerminalPrefs.allKeys.map(\.id).filter(set.contains).joined(separator: ",")
        })
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

/// Completions for the terminal's text field from what the pane shows: commands typed after a prompt earlier on, and
/// words (paths, names) for the one being typed. Latest first.
enum TerminalCompletion {
    static let prompts = ["$ ", "% ", "# ", "❯ ", "➜ ", "> ", "» "]

    static func suggestions(for input: String, rows: [TermRow], limit: Int = 6) -> [String] {
        guard !input.isEmpty else { return [] }
        let lines = rows.suffix(600).map(TerminalGrid.plain).reversed()
        var out: [String] = []
        func add(_ s: String) {
            if s != input, !out.contains(s), out.count < limit { out.append(s) }
        }
        for line in lines {
            if let cmd = command(in: line), cmd.hasPrefix(input) { add(cmd) }
        }
        let word = input.split(separator: " ", omittingEmptySubsequences: false).last.map(String.init) ?? ""
        if word.count >= 2 {
            let head = String(input.dropLast(word.count))
            for line in lines {
                for token in line.split(whereSeparator: { $0 == " " || $0 == "\"" || $0 == "'" || $0 == "(" || $0 == ")" })
                where token.count > word.count && token.count <= 120 && token.hasPrefix(word) {
                    add(head + token)
                }
            }
        }
        return out
    }

    /// What follows a prompt at the start of a line ("~/app $ git status" → "git status").
    static func command(in line: String) -> String? {
        let head = line.prefix(60)
        for p in prompts {
            guard let r = head.range(of: p) else { continue }
            let cmd = line[r.upperBound...].trimmingCharacters(in: .whitespaces)
            return cmd.isEmpty || cmd.count > 200 ? nil : cmd
        }
        return nil
    }
}

/// Completion chips over the text field.
struct SuggestionStrip: View {
    var items: [String]
    var pick: (String) -> Void

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 6) {
                ForEach(items, id: \.self) { s in
                    Button { Haptics.tap(); pick(s) } label: {
                        Text(s).font(.system(.footnote, design: .monospaced)).lineLimit(1)
                            .padding(.horizontal, 10).padding(.vertical, 6)
                            .background(Color.primary.opacity(0.07), in: .capsule)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .scrollClipDisabled()
    }
}
