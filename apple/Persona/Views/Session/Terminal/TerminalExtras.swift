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
    /// Mac: Option+key sends ESC+key (Meta), like Terminal.app's setting of that name; off by default, as there.
    static let optionAsMeta = "terminalOptionAsMeta"
    /// Mac: who gets the mouse while driving, "program" (the default) or "local" (MacTerminalMouse.Mode).
    static let mouseMode = "terminalMouseMode"
    /// "phone": the phone drives the terminal at its own size while the tab is open; "mirror": it shows the pane at the
    /// computer's size and folds long rows.
    static let size = "terminalSize"
    static let defaultFontSize = 15.0 // as Moshi reads: about 46 columns on a 6.9-inch phone
    static let defaultShortcuts = ["git status", "git diff --stat", "ls -la", "/compact", "/status"]
    static let allKeys: [(id: String, label: String)] = [("esc", "esc"), ("tab", "tab"), ("ctrlc", "⌃C"), ("shortcuts", String(localized: "快捷命令")),
                                                         ("paste", String(localized: "粘贴")), ("files", String(localized: "照片 / 文件")), ("alt", "alt"), ("shift", "⇧ shift"), ("pad", String(localized: "方向键"))]
    /// alt and shift are a tap away in the settings (Moshi shows only ctrl too): the rest fits a phone's width. So is
    /// the photos / files key: holding paste offers the same.
    static let defaultKeys = allKeys.map(\.id).filter { !["alt", "shift", "files"].contains($0) }.joined(separator: ",")

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
    var hint = String(localized: "快捷 · 单击填入，双击发送")
    var fill: (String) -> Void
    var send: (String) -> Void
    var edit: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: Theme.Space.s) {
                Text(hint).font(Theme.Font.labelStrong).foregroundStyle(.secondary).lineLimit(1)
                Spacer(minLength: 4)
                Button("编辑", action: edit).font(Theme.Font.labelStrong).lineLimit(1)
            }
            .padding(.horizontal, Theme.Space.l)
            .padding(.vertical, Theme.Space.m)
            if items.isEmpty {
                Text("还没有快捷命令，点「编辑」添加").font(Theme.Font.caption).foregroundStyle(.secondary)
                    .padding(.horizontal, Theme.Space.l).padding(.bottom, Theme.Space.m)
            }
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(Array(items.enumerated()), id: \.offset) { _, item in
                        Text(item)
                            .font(Theme.Font.secondary.monospaced())
                            .lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, Theme.Space.l)
                            .padding(.vertical, Theme.Space.m)
                            .contentShape(.rect)
                            .onTapGesture(count: 2) { Haptics.success(); send(item) }
                            .onTapGesture { Haptics.tap(); fill(item) }
                            .accessibilityAddTraits(.isButton)
                            .accessibilityHint("单击填入输入框，双击直接发送")
                            .accessibilityAction(named: "直接发送") { send(item) }
                        Divider().padding(.leading, Theme.Space.l)
                    }
                }
            }
            .frame(maxHeight: 230)
            .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: 360)
        .surface(in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous)) // no shadow over the surface
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
    #if os(macOS)
    @AppStorage(TerminalPrefs.optionAsMeta) private var optionAsMeta = false
    @AppStorage(TerminalPrefs.mouseMode) private var mouseMode = MacTerminalMouse.Mode.program.rawValue
    #endif
    @State private var newShortcut = ""

    private var shortcuts: Binding<[String]> {
        Binding(get: { TerminalPrefs.decode(shortcutsJSON) }, set: { shortcutsJSON = TerminalPrefs.encode($0) })
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SegmentedChoice(String(localized: "终端尺寸"), [("phone", TerminalSizeText.own), ("mirror", String(localized: "电脑尺寸"))], selection: $size)
                } header: {
                    Text("尺寸")
                } footer: {
                    Text(size == "phone" ? TerminalSizeText.ownFooter : TerminalSizeText.mirrorFooter)
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
                    Text("墨黑、深夜不随系统变化；Persona、Solarized 跟着系统的浅色 / 深色。")
                }
                Section {
                    Picker("字体", selection: $fontID) {
                        ForEach(TerminalFont.allCases, id: \.rawValue) { Text($0.name).tag($0.rawValue) }
                    }
                    Stepper(value: $fontSize, in: 7...20, step: 0.5) {
                        Text("字号 \(fontSize, specifier: "%.1f")").monospacedDigit()
                    }
                    Toggle(TerminalSizeText.wrap, isOn: $wrap)
                } header: {
                    Text("文字")
                } footer: {
                    Text(TerminalHelpText.textFooter)
                }
                Section {
                    SegmentedChoice(String(localized: "输入方式"), [("direct", String(localized: "直接打字")), ("field", String(localized: "输入框"))], selection: $inputMode)
                } header: {
                    Text("输入")
                } footer: {
                    Text(inputMode == "field" ? String(localized: "在终端下方的输入框里写好，按回车一次发出去；键栏的 ctrl / alt / ⇧ 对输入框里打的下一个字也生效。")
                         : TerminalHelpText.directFooter)
                }
                #if os(macOS)
                Section {
                    Toggle("将 Option 键用作 Meta 键", isOn: $optionAsMeta)
                } header: {
                    Text("键盘")
                } footer: {
                    Text(optionAsMeta ? "Option+字母发出 Esc+字母，shell 里 Option+B / Option+F 按词移动；Option 打不出 ™、é 这类特殊字符了。输入法照常用。"
                         : "Option 照常打出特殊字符（Option+2 是 ™）；打开后 Option+键 发出 Esc+键，和「终端」App 的同名设置一样。")
                }
                Section {
                    SegmentedChoice(String(localized: "接管时的鼠标"), [(MacTerminalMouse.Mode.program.rawValue, String(localized: "交给程序")), (MacTerminalMouse.Mode.local.rawValue, String(localized: "本地选择"))],
                                    selection: $mouseMode)
                } header: {
                    Text("鼠标")
                } footer: {
                    Text(MacTerminalMouse.mode(mouseMode) == .program
                         ? "接管时点击、拖动、右键和中键都交给电脑上的程序（vim、Claude Code 这类开了鼠标的程序才收得到），按住 Shift 在这里选字、右键出菜单；⌘ 点链接照旧打开。"
                         : "接管时在这里选字、右键出菜单，单击照旧是点一下程序；按住 Shift 才把鼠标交给程序。")
                }
                #endif
                Section {
                    ForEach(Array(shortcuts.wrappedValue.enumerated()), id: \.offset) { i, s in
                        Text(s).font(Theme.Font.body.monospaced()).lineLimit(1)
                        #if os(macOS)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .contentShape(.rect)
                            .contextMenu { shortcutMenu(i) }
                        #endif
                    }
                    .onDelete { shortcuts.wrappedValue.remove(atOffsets: $0) }
                    .onMove { shortcuts.wrappedValue.move(fromOffsets: $0, toOffset: $1) }
                    HStack {
                        TextField("新的快捷命令", text: $newShortcut)
                            .font(Theme.Font.body.monospaced())
                            .textInputAutocapitalization(.never)
                            .autocorrectionDisabled()
                            .onSubmit(add)
                        Button("添加", action: add).lineLimit(1).disabled(newShortcut.trimmingCharacters(in: .whitespaces).isEmpty)
                    }
                } header: {
                    Text("快捷命令")
                } footer: {
                    Text(TerminalHelpText.shortcutsFooter)
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
                    Text(TerminalHelpText.keysFooter)
                }
                #if os(macOS)
                Section("操作") {
                    ForEach(TerminalHelpText.gestures) { Label($0.text, systemImage: $0.symbol) }
                }
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                #else
                Section("手势") {
                    Label("点终端：点在终端里（Claude 的「Jump to bottom」、顶上的提问）", systemImage: "hand.tap")
                    Label("上下拖：在终端里滚动（Claude 往回翻对话）", systemImage: "arrow.up.and.down")
                    Label("键栏的键盘按钮：弹出键盘；双击是回车", systemImage: Theme.Icon.keyboard)
                    Label("捏合调字号", systemImage: Theme.Icon.enlarge)
                    Label("双指左右滑：切换同一空间里的 Agent", systemImage: "arrow.left.arrow.right")
                    Label("双指上下滑：切换到相邻的空间", systemImage: Theme.Icon.upDown)
                    Label("双击终端：粘贴剪贴板", systemImage: Theme.Icon.paste)
                    Label("长按：选择、拷贝", systemImage: Theme.Icon.insert)
                    Label("听写：键盘上的麦克风", systemImage: Theme.Icon.dictate)
                }
                .font(Theme.Font.secondary)
                .foregroundStyle(.secondary)
                #endif
            }
            .themedPage()
            .navigationTitle("终端设置")
            .navigationBarTitleDisplayMode(.inline)
            .pageToolbar {
                ToolbarItem(placement: .topBarLeading) { EditButton() }
                ToolbarItem(placement: .confirmationAction) { Button("完成") { dismiss() } }
            }
        }
    }

    #if os(macOS)
    /// A Mac list has no edit mode (`EditButton` is empty there): a command moves or goes from its context menu.
    @ViewBuilder private func shortcutMenu(_ i: Int) -> some View {
        let list = shortcuts.wrappedValue
        Button { shortcuts.wrappedValue = MacShortcutOrder.move(list, at: i, by: -1) } label: { Label("上移", systemImage: Theme.Icon.moveUp) }
            .disabled(i == 0)
        Button { shortcuts.wrappedValue = MacShortcutOrder.move(list, at: i, by: 1) } label: { Label("下移", systemImage: Theme.Icon.moveDown) }
            .disabled(i == list.count - 1)
        Divider()
        Button(role: .destructive) { shortcuts.wrappedValue.remove(at: i) } label: { Label("删除", systemImage: Theme.Icon.delete) }
    }
    #endif

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
        let ansi = "\u{1B}[38;2;215;119;87m⏺\u{1B}[0m " + String(localized: "好的，先跑一下测试：", comment: "Sample terminal output in the terminal settings preview") + "\r\n"
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
        HStack(spacing: Theme.Space.m) {
            Text(theme.name).lineLimit(1)
            Spacer(minLength: 8)
            HStack(spacing: Theme.Space.xs) {
                ForEach([side.background, side.foreground, side.ansi[1], side.ansi[2], side.ansi[4]], id: \.self) { c in
                    Circle().fill(Color(hex: c)).scaledFrame(width: Theme.Size.markS, height: Theme.Size.markS)
                        .overlay(Circle().stroke(Color.primary.opacity(0.15), lineWidth: 0.5))
                }
            }
            Image(systemName: Theme.Icon.done).font(Theme.Font.heading).foregroundStyle(Theme.accent).opacity(on ? 1 : 0)
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
            HStack(spacing: Theme.Space.sm) {
                ForEach(items, id: \.self) { s in
                    Button { Haptics.tap(); pick(s) } label: {
                        Text(s).font(Theme.Font.code).lineLimit(1)
                            .padding(.horizontal, Theme.Space.m).padding(.vertical, Theme.Space.sm)
                            .background(Theme.Fill.chip, in: .capsule)
                    }
                    .buttonStyle(.plain)
                }
            }
        }
        .scrollClipDisabled()
    }
}

/// The terminal's size wording: the phone's size on iOS, the window's on the Mac (a Mac takes over at its panel's size).
enum TerminalSizeText {
    #if os(macOS)
    static let own = String(localized: "窗口尺寸")
    static let ownFooter = String(localized: "打开终端时由这个窗口接管：电脑上的窗格变成窗口的行列数，Claude 按窗口宽度排版；离开终端或断线后，窗格回到电脑上的尺寸。")
    static let mirrorFooter = String(localized: "不改电脑上的窗格，按电脑的宽度显示，长行在窗口里折行（或左右滚动）。")
    static let wrap = String(localized: "长行按窗口宽度换行")
    static let takeOver = String(localized: "比如 Ovis 或另一个 herdr 客户端。接管后它会断开，终端改成这个窗口的尺寸；离开终端时窗格回到电脑上的尺寸。")
    static let icon = Theme.Icon.laptop
    static let takeOverIcon = Theme.Icon.takeOverWindow
    #else
    static let own = String(localized: "手机尺寸")
    static let ownFooter = String(localized: "打开终端时由手机接管：电脑上的窗格变成手机的行列数，Claude 按手机宽度排版；离开终端、App 进后台或断线后，窗格回到电脑上的尺寸。")
    static let mirrorFooter = String(localized: "不改电脑上的窗格，按电脑的宽度显示，长行在手机上折行（或左右滑）。")
    static let wrap = String(localized: "长行按手机宽度换行")
    static let takeOver = String(localized: "比如 Ovis 或另一个 herdr 客户端。接管后它会断开，终端改成手机的尺寸；离开终端时窗格回到电脑上的尺寸。")
    static let icon = Theme.Icon.phone
    static let takeOverIcon = Theme.Icon.handoff
    #endif
}

/// The terminal's help wording: touches on iOS; on the Mac a click, the keyboard, the wheel and the trackpad.
enum TerminalHelpText {
    struct Gesture: Identifiable {
        var text: String
        var symbol: String
        var id: String { text }
    }

    #if os(macOS)
    static let loadOlder = String(localized: "往上滚动加载更早的输出")
    static let noWrap = String(localized: "不换行 · 左右滚动")
    static let noWrapValue = String(localized: "关，左右滚动看")
    static let textFooter = String(localized: "⌘+ / ⌘− / ⌘0 或在触控板上捏合也能调字号。中文用系统字体，每个字占两格。")
    static let directFooter = String(localized: "点一下终端就能直接打字，每个键发到终端（输入法也能用），⌘ 组合键留给 App；键栏的键盘按钮把输入移到终端或移开，双击它是回车。点终端同时是点在终端里（比如 Claude 的「Jump to bottom」）。")
    static let shortcutsFooter = String(localized: "在键栏的闪电里：单击输入，双击带回车执行。右键一条可以上移、下移或删除。")
    static let keysFooter = String(localized: "ctrl 一直在，键盘按钮和方向键在最右。放不下时键栏可以左右滚动。")
    static let gestures: [Gesture] = [
        Gesture(text: String(localized: "点终端：开始打字，也是点在终端里（Claude 的「Jump to bottom」、顶上的提问）"), symbol: Theme.Icon.click),
        Gesture(text: String(localized: "滚轮或触控板：在终端里滚动（Claude 往回翻对话）"), symbol: Theme.Icon.upDown),
        Gesture(text: String(localized: "拖动选择，⌘C 拷贝；右键菜单里有拷贝、粘贴、全选"), symbol: Theme.Icon.contextMenu),
        Gesture(text: String(localized: "⌘V：粘贴剪贴板（多行先问一下）"), symbol: Theme.Icon.paste),
        Gesture(text: String(localized: "⌘+ / ⌘− / ⌘0、触控板捏合：调字号"), symbol: Theme.Icon.enlarge),
        Gesture(text: String(localized: "触控板三指左右轻扫：切换同一空间里的 Agent（系统设置 › 触控板里「在页面之间轻扫」选三指时）"), symbol: Theme.Icon.swipe),
    ]
    #else
    static let loadOlder = String(localized: "上滑加载更早的输出")
    static let noWrap = String(localized: "不换行 · 左右滑")
    static let noWrapValue = String(localized: "关，左右滑动看")
    static let textFooter = String(localized: "在终端上双指捏合也能调字号。中文用系统字体，每个字占两格。")
    static let directFooter = String(localized: "点键栏右边的键盘按钮弹出键盘，每个键直接发到终端（像在电脑上一样，输入法、听写都能用）；双击键盘按钮是回车。点终端是点在终端里（比如 Claude 的「Jump to bottom」），不会弹键盘。")
    static let shortcutsFooter = String(localized: "在键栏的闪电里：单击输入，双击带回车执行。左上角「编辑」可以删除、排序。")
    static let keysFooter = String(localized: "ctrl 一直在，键盘按钮和方向键在最右。放不下时键栏可以左右滑。")
    #endif
    #if os(macOS)

    /// Everything above, for the wording check (MacTerminalWordingTests).
    static var all: [String] {
        [loadOlder, noWrap, noWrapValue, textFooter, directFooter, shortcutsFooter, keysFooter] + gestures.map(\.text)
    }
    #endif
}
