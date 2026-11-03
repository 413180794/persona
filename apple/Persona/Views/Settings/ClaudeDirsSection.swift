import SwiftUI

/// The pure part of 屏蔽的配置目录 (web `lib/agent-settings.ts` does the same): what a row says, and the rules after a
/// change. Blocking a row adds its path; unblocking lifts the rule that blocks it (a glob lifts every directory it covers).
nonisolated enum ClaudeDirsLogic {
    typealias Dir = APIClient.ClaudeDir
    typealias State = APIClient.ClaudeDirsState

    /// The path as the user reads it: the machine's home as ~.
    static func shortPath(_ path: String, home: String?) -> String {
        guard let home, !home.isEmpty, path == home || path.hasPrefix(home + "/") else { return path }
        return "~" + path.dropFirst(home.count)
    }

    static func samePath(_ pattern: String, _ path: String, home: String?) -> Bool {
        pattern == path || pattern == shortPath(path, home: home)
    }

    /// Under the path: where it came from, how many sessions, how many claudes run from it, the rule blocking it.
    static func meta(_ d: Dir, home: String?) -> String {
        var parts: [String] = []
        if d.isDefault == true { parts.append("默认目录，不能屏蔽") }
        else if d.source == "config" { parts.append("hub.json 里配的") }
        else { parts.append("自动发现") }
        if let n = d.sessions, n > 0 { parts.append("\(n) 个会话") }
        if d.running > 0 { parts.append("\(d.running) 个正在运行") }
        if d.blocked == true, let by = d.blockedBy, !by.isEmpty, !samePath(by, d.path, home: home) { parts.append("由规则 \(by) 屏蔽") }
        return parts.joined(separator: " · ")
    }

    static func toggled(_ s: State, _ d: Dir) -> [String] {
        if d.blocked != true {
            return s.blocked.contains(d.path) ? s.blocked : s.blocked + [shortPath(d.path, home: s.home)]
        }
        let rule = d.blockedBy ?? d.path
        return s.blocked.filter { $0 != rule }
    }

    /// Rules that block none of the listed directories: listed on their own, to be deleted.
    static func looseRules(_ s: State) -> [String] {
        s.blocked.filter { p in !s.dirs.contains { $0.blocked == true && ($0.blockedBy == p || samePath(p, $0.path, home: s.home)) } }
    }

    static func cleaned(_ input: String) -> String {
        var p = input.trimmingCharacters(in: .whitespacesAndNewlines)
        while p.count > 1, p.hasSuffix("/") { p.removeLast() }
        return p
    }

    /// Why a typed rule cannot be added (nil = it can).
    static func problem(_ input: String, existing: [String]) -> String? {
        let p = cleaned(input)
        if p.isEmpty { return "填一个目录或通配，例如 ~/.automation/*" }
        if !p.hasPrefix("/") && !p.hasPrefix("~/") { return "要以 / 或 ~/ 开头（~ 是那台电脑的家目录）" }
        if p == "~" || p == "/" { return "不能屏蔽整个家目录或根目录" }
        if existing.contains(p) { return "已经屏蔽了" }
        return nil
    }
}

/// 设置 › Claude Code › 屏蔽的配置目录 (docs/claude-integration.md「屏蔽配置目录」): the machine's Claude Code config
/// directories, each with 屏蔽 / 取消屏蔽, and rules typed by hand (a path or a glob such as ~/.automation/*). A blocked
/// directory is not scanned or remembered and its sessions leave Agents; the files are not touched. Sections of the
/// page's List.
struct ClaudeDirsSection: View {
    @Environment(AppModel.self) private var model
    var ref: MachineRef
    @State private var state: APIClient.ClaudeDirsState?
    @State private var error: String?
    @State private var busy: String?
    @State private var input = ""

    private typealias L = ClaudeDirsLogic

    var body: some View {
        Section {
            if let error {
                Text(error).foregroundStyle(Theme.danger).font(Theme.Font.caption)
            }
            if let state {
                ForEach(state.dirs) { d in
                    row(L.shortPath(d.path, home: state.home), meta: L.meta(d, home: state.home), blocked: d.blocked == true) {
                        if d.isDefault != true {
                            Button(d.blocked == true ? "取消屏蔽" : "屏蔽") {
                                let label = L.shortPath(d.path, home: state.home)
                                Task { await save(L.toggled(state, d), key: d.path, done: d.blocked == true ? "已取消屏蔽 \(d.blockedBy ?? label)" : "已屏蔽 \(label)") }
                            }
                            .lineLimit(1)
                            .themedButton()
                            .disabled(busy != nil)
                        }
                    }
                }
                ForEach(L.looseRules(state), id: \.self) { p in
                    row(p, meta: "规则", blocked: true) {
                        Button("删除", role: .destructive) {
                            Task { await save(state.blocked.filter { $0 != p }, key: p, done: "已删除规则 \(p)") }
                        }
                        .lineLimit(1)
                        .themedButton()
                        .disabled(busy != nil)
                    }
                }
                addRow(state)
            } else if error == nil {
                ProgressView().frame(maxWidth: .infinity)
            }
        } header: {
            Text("屏蔽的配置目录")
        } footer: {
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                Text("被屏蔽的目录不再扫描、不再记住，里面的会话不出现在 Agents（文件不动）；取消屏蔽后恢复。适合用单独 CLAUDE_CONFIG_DIR 跑 claude 的自动化脚本。默认目录不能屏蔽。")
                if state?.live == false { Text("这台电脑不在线或 node 较旧：列的是 hub 记下的目录，连上后会列出全部。") }
            }
        }
        .task(id: ref.path) { await load() }
    }

    private func row<Trailing: View>(_ title: String, meta: String, blocked: Bool, @ViewBuilder trailing: () -> Trailing) -> some View {
        HStack(spacing: Theme.Space.m) {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(verbatim: title)
                    .font(Theme.Font.code)
                    .strikethrough(blocked)
                    .foregroundStyle(blocked ? .secondary : .primary)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(meta).font(Theme.Font.label).foregroundStyle(.secondary)
            }
            Spacer(minLength: Theme.Space.s)
            trailing()
        }
        .padding(.vertical, Theme.Space.xxs)
    }

    @ViewBuilder private func addRow(_ state: APIClient.ClaudeDirsState) -> some View {
        let problem = L.problem(input, existing: state.blocked)
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(spacing: Theme.Space.m) {
                TextField("~/.automation/* 或具体路径", text: $input)
                    .font(Theme.Font.code)
                    .autocorrectionDisabled()
                    #if os(iOS)
                    .textInputAutocapitalization(.never)
                    #endif
                    .submitLabel(.done)
                    .onSubmit { Task { await add(state) } }
                Button { Task { await add(state) } } label: {
                    Label("屏蔽", systemImage: Theme.Icon.add).lineLimit(1)
                }
                .themedButton()
                .disabled(input.isEmpty || problem != nil || busy != nil)
            }
            if !input.isEmpty, let problem {
                Text(problem).font(Theme.Font.label).foregroundStyle(.secondary)
            }
        }
    }

    private func add(_ state: APIClient.ClaudeDirsState) async {
        guard L.problem(input, existing: state.blocked) == nil else { return }
        let p = L.cleaned(input)
        if await save(state.blocked + [p], key: "+", done: "已屏蔽 \(p)") { input = "" }
    }

    private func load() async {
        guard let api = model.api(ref.group) else { return }
        do {
            state = try await api.claudeDirs(machine: ref.id)
            error = nil
        } catch {
            if !error.isCancellation, let shown = error.shown { self.error = "读不到这台电脑的配置目录：\(shown)" }
        }
    }

    @discardableResult private func save(_ blocked: [String], key: String, done: String) async -> Bool {
        guard let api = model.api(ref.group) else { return false }
        busy = key
        defer { busy = nil }
        do {
            state = try await api.setBlockedClaudeDirs(machine: ref.id, blocked: blocked)
            error = nil
            Haptics.success()
            model.toast = Toast(done, style: .success)
            return true
        } catch {
            if !error.isCancellation {
                Haptics.error()
                self.error = "没有改成：\(error.shown ?? error.localizedDescription)"
            }
            return false
        }
    }
}
