import SwiftUI

// The terminal tab's operations layer: saved shortcuts, the key bar's buttons, appearance, and suggestions taken from
// what the pane shows (never the computer's shell history).

/// Settings kept on the phone (`@AppStorage`).
enum TerminalPrefs {
    static let shortcuts = "terminalShortcuts" // JSON [String]
    static let keys = "terminalKeys" // the optional key bar buttons shown, comma separated
    static let autoHide = "terminalAutoHideKeys"
    static let defaultShortcuts = ["git status", "git diff --stat", "ls -la", "/compact", "/status"]
    static let allKeys: [(id: String, label: String)] = [("alt", "alt"), ("shift", "⇧ shift"), ("esc", "esc"), ("tab", "tab"),
                                                         ("ctrlc", "⌃C"), ("pad", "方向键")]
    static let defaultKeys = allKeys.map(\.id).joined(separator: ",")

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
    var fill: (String) -> Void
    var send: (String) -> Void
    var edit: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Text("快捷 · 单击填入，双击发送").font(.caption.weight(.semibold)).foregroundStyle(.secondary).lineLimit(1)
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
    @AppStorage("terminalWrap") private var wrap = true
    @AppStorage("terminalFontSize") private var fontSize = 11.0
    @AppStorage("terminalTheme") private var themeID = TerminalTheme.jarvis.id
    @State private var newShortcut = ""

    private var shortcuts: Binding<[String]> {
        Binding(get: { TerminalPrefs.decode(shortcutsJSON) }, set: { shortcutsJSON = TerminalPrefs.encode($0) })
    }

    var body: some View {
        NavigationStack {
            Form {
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
                    Text("在终端输入框左边的闪电里：单击填入，双击直接发送。")
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
                    Text("ctrl 一直在。收起时点输入框或按下修饰键会再出现。")
                }
                Section("显示") {
                    Picker("配色", selection: $themeID) {
                        ForEach(TerminalTheme.all, id: \.id) { Text($0.name).tag($0.id) }
                    }
                    Stepper(value: $fontSize, in: 7...20, step: 0.5) {
                        Text("字号 \(fontSize, specifier: "%.1f")").monospacedDigit()
                    }
                    Toggle("长行按手机宽度换行", isOn: $wrap)
                }
                Section("手势") {
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
