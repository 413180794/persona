import SwiftUI

/// Claude Code's permission modes and effort levels as the app shows them (server: node/modes.go).
nonisolated enum SessionModes {
    struct Option: Identifiable, Hashable {
        var id: String
        var title: String
        var detail: String
        var symbol: String
    }

    static let permissionModes: [Option] = [
        Option(id: "default", title: "手动确认", detail: "改文件、跑命令前都先问你", symbol: Theme.Icon.askFirst),
        Option(id: "acceptEdits", title: "自动改文件", detail: "改文件不问，跑命令还会问", symbol: "pencil.line"),
        Option(id: "plan", title: "只做计划", detail: "只读代码、写计划，批准后才动手", symbol: "list.bullet.clipboard"),
        Option(id: "auto", title: "自动", detail: "自己判断风险，安全的直接做", symbol: "wand.and.sparkles"),
        Option(id: "bypassPermissions", title: "全部放行", detail: "什么都不问；只能在新开时选", symbol: Theme.Icon.allowEverything),
    ]

    static let effortLevels: [Option] = [
        Option(id: "low", title: "低", detail: "最快，简单的事够用", symbol: "gauge.with.dots.needle.0percent"),
        Option(id: "medium", title: "中", detail: "快一些", symbol: Theme.Icon.context),
        Option(id: "high", title: "高", detail: "大多数任务的默认", symbol: Theme.Icon.effort),
        Option(id: "xhigh", title: "很高", detail: "更仔细，也更慢、更贵", symbol: "gauge.with.dots.needle.67percent"),
        Option(id: "max", title: "最高", detail: "最慢最仔细", symbol: "gauge.with.dots.needle.100percent"),
    ]

    /// Codex's reasoning efforts (node/codex_effort.go); a thread offers its model's, in `Session.efforts`.
    static let codexEffortLevels: [Option] = [
        Option(id: "minimal", title: "极低", detail: "几乎不思考，最快", symbol: effortLevels[0].symbol),
        Option(id: "low", title: "低", detail: "快，简单的事够用", symbol: effortLevels[0].symbol),
        Option(id: "medium", title: "中", detail: "快慢适中", symbol: effortLevels[1].symbol),
        Option(id: "high", title: "高", detail: "更仔细", symbol: effortLevels[2].symbol),
        Option(id: "xhigh", title: "很高", detail: "更仔细，也更慢、更贵", symbol: effortLevels[3].symbol),
        Option(id: "max", title: "最高", detail: "最慢最仔细", symbol: effortLevels[4].symbol),
        Option(id: "ultra", title: "极高", detail: "最仔细，还会主动分给多个子 Agent 一起做", symbol: effortLevels[4].symbol),
    ]

    /// Modes a running session can switch to (bypassPermissions only at start, see node/modes.go).
    static let switchableModes = permissionModes.filter { $0.id != "bypassPermissions" }

    static func mode(_ id: String?) -> Option? { permissionModes.first { $0.id == id } }
    static func effort(_ id: String?) -> Option? { effortLevels.first { $0.id == id } ?? codexEffortLevels.first { $0.id == id } }

    /// A running session's effort choices: its own levels when the hub lists them (a Codex thread on its app-server),
    /// Claude Code's otherwise; a level this app does not know yet keeps its id.
    static func effortOptions(_ session: Session?) -> [Option] {
        guard let ids = session?.efforts, !ids.isEmpty else { return effortLevels }
        return ids.map { id in codexEffortLevels.first { $0.id == id } ?? Option(id: id, title: id, detail: "", symbol: Theme.Icon.effort) }
    }

    static func modeTint(_ id: String?) -> Color {
        switch id {
        // Neutral: amber is the working state and would read as 等你处理. Only "allow everything" warns.
        case "bypassPermissions": Theme.danger
        default: .secondary
        }
    }

    /// "手动确认" for known modes, the raw value otherwise (Codex approval policies, newer Claude Code modes).
    static func modeLabel(_ id: String?) -> String? { id.map { mode($0)?.title ?? $0 } }
    static func effortLabel(_ id: String?) -> String? { id.map { effort($0)?.title ?? $0 } }

    /// The shell composer's model button's two words (#1091, #1117), ChatGPT's "6.1 Sol 中": the model, drawn in the text's
    /// ink, and the effort, in the quieter ink after it.
    struct ComposerTitle: Equatable, Sendable {
        var model: String
        var effort: String?
        /// For VoiceOver and the UI tests: "Opus 5.5 高".
        var spoken: String { effort.map { "\(model) \($0)" } ?? model }
    }

    /// "Opus 5.5" + "高"; the model alone; "思考" + "高" with no model known; nil with neither.
    static func composerTitle(model: String?, effort: String?) -> ComposerTitle? {
        switch (model, effort) {
        case let (m?, e): ComposerTitle(model: m, effort: e)
        case let (nil, e?): ComposerTitle(model: "思考", effort: e)
        case (nil, nil): nil
        }
    }

    /// GPT display names only: keep the version and every variant, without changing the id used by the agent.
    static func gptModelName(_ model: String?) -> String? {
        guard let raw = Fmt.model(model)?.trimmingCharacters(in: .whitespacesAndNewlines),
              raw.lowercased().hasPrefix("gpt-") else { return nil }
        let parts = raw.dropFirst(4).split(separator: "-", omittingEmptySubsequences: false)
        guard !parts.isEmpty, parts.allSatisfy({ !$0.isEmpty }) else { return nil }
        return parts.map { part in
            guard let first = part.first, first.isLetter else { return String(part) }
            return String(first).uppercased() + part.dropFirst().lowercased()
        }.joined(separator: " ")
    }

    /// A model's short name for that button: "claude-opus-5-5-20260101" → "Opus 5.5", "opus" / "opus[1m]" → "Opus",
    /// "gpt-6.1-sol" → "6.1 Sol"; other names retain the existing `Fmt.model` behavior.
    static func modelName(_ model: String?) -> String? {
        guard let model, !model.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              let raw = Fmt.model(model) else { return nil }
        if let gpt = gptModelName(raw) { return gpt }
        let lower = raw.lowercased()
        for family in ["fable", "opus", "sonnet", "haiku"] {
            guard let r = lower.range(of: family) else { continue }
            // The version follows the family in today's ids; a date (8 digits) and older ids' leading version do not.
            let after = lower[r.upperBound...].split { $0 == "-" || $0 == "." || $0 == "_" }
            let version = after.prefix { $0.allSatisfy(\.isNumber) && $0.count <= 2 }.prefix(2).joined(separator: ".")
            return family.capitalized + (version.isEmpty ? "" : " " + version)
        }
        return raw
    }
}

/// The two chips above the composer — permission mode and effort level, like the footer of Claude Code's TUI.
/// Tapping one changes it on the running session. A Codex thread on its app-server gets the effort alone, among its own
/// levels (`Session.setsEffortOnly`).
struct SessionModeBar: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var session: Session?
    @State private var changing = false

    var body: some View {
        #if DEBUG || PERF_SIGNPOST
        let _ = PerfTrace.body("SessionModeBar")
        #endif
        let mode = session?.permissionMode
        let effort = session?.effort
        HStack(spacing: Theme.Space.s) {
            if session?.setsEffortOnly != true {
                Menu {
                    Picker("权限模式", selection: Binding(get: { mode ?? "" }, set: { new in change(mode: new) })) {
                        ForEach(options(SessionModes.switchableModes, current: mode)) { o in
                            Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id)
                        }
                    }
                    .pickerStyle(.inline)
                } label: {
                    ModeChip(symbol: SessionModes.mode(mode)?.symbol ?? Theme.Icon.askFirst,
                             text: SessionModes.modeLabel(mode) ?? "权限模式", tint: SessionModes.modeTint(mode), known: mode != nil)
                }
                .labelOnlyMenu()
                .accessibilityLabel("权限模式：\(SessionModes.modeLabel(mode) ?? "未知")")
            }
            Menu {
                Picker("思考强度", selection: Binding(get: { effort ?? "" }, set: { new in change(effort: new) })) {
                    ForEach(SessionModes.effortOptions(session)) { o in
                        Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id)
                    }
                }
                .pickerStyle(.inline)
            } label: {
                ModeChip(symbol: SessionModes.effort(effort)?.symbol ?? Theme.Icon.effort,
                         text: effort.map { "思考\(SessionModes.effortLabel($0) ?? $0)" } ?? "思考强度", tint: .secondary, known: effort != nil)
            }
            .labelOnlyMenu()
            .accessibilityLabel("思考强度：\(SessionModes.effortLabel(effort) ?? "未知")")
            if changing { ProgressView().controlSize(.mini) }
            Spacer(minLength: 0)
        }
        .disabled(changing)
        .animation(Theme.Motion.standard, value: changing)
    }

    /// The switchable options plus the current one when it is not among them (bypassPermissions, an unknown mode).
    private func options(_ list: [SessionModes.Option], current: String?) -> [SessionModes.Option] {
        guard let current, !list.contains(where: { $0.id == current }) else { return list }
        return list + [SessionModes.mode(current) ?? .init(id: current, title: current, detail: "", symbol: Theme.Icon.help)]
    }

    private func change(mode: String? = nil, effort: String? = nil) {
        guard mode != session?.permissionMode || effort != session?.effort, !changing else { return }
        changing = true
        Task {
            await model.setModes(key, permissionMode: mode, effort: effort)
            changing = false
        }
    }
}

/// The shell composer's model menu for a running conversation (#1091): the model (a choice where Claude Code takes
/// `/model`, otherwise the one it runs), the effort, and the permission mode a level down. Effort and mode are the same
/// choices as `SessionModeBar`, which the old layout keeps above its composer.
struct SessionModesMenu: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var session: Session?
    /// Claude Code in a herdr pane: picking a model sends `/model <alias>`.
    var picksModel = false
    /// Effort and permission mode can change (Claude Code, the assistant; not Codex or TRAEX).
    var setsModes = true
    /// Only the effort can change, among the session's own levels (a Codex thread on its app-server, `Session.setsEffortOnly`).
    var setsEffortOnly = false

    /// The button's title for this conversation.
    static func title(_ session: Session?, setsModes: Bool) -> SessionModes.ComposerTitle? {
        SessionModes.composerTitle(model: SessionModes.modelName(session?.model),
                                   effort: setsModes ? SessionModes.effortLabel(session?.effort) : nil)
    }

    var body: some View {
        let mode = session?.permissionMode
        let effort = session?.effort
        // 全部放行 is said here, not on the composer's button (#1117). First: a menu opening upward from the button puts
        // its first item next to it, where a menu too tall for the screen is never cut off.
        if setsModes, mode == "bypassPermissions" {
            Section { BypassWarning() }
        }
        Section("模型") {
            if picksModel {
                Picker("模型", selection: Binding(get: { PromptItems.claudeModelID(session?.model) }, set: { id in
                    guard id != PromptItems.claudeModelID(session?.model) else { return }
                    model.sendNow(key, text: "/model \(id)", images: []) // photos and quotes stay for the message
                })) {
                    ForEach(PromptItems.claudeModels, id: \.id) { m in
                        Text(m.name).tag(m.id)
                    }
                }
                .pickerStyle(.inline)
            } else {
                Text(SessionModes.modelName(session?.model) ?? "模型未知") // what it runs; changed where it was started
            }
        }
        if setsModes || setsEffortOnly {
            Section("思考强度") {
                Picker("思考强度", selection: Binding(get: { effort ?? "" }, set: { new in change(effort: new) })) {
                    ForEach(SessionModes.effortOptions(session)) { o in
                        Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id)
                    }
                }
                .pickerStyle(.inline)
            }
        }
        if setsModes {
            Picker(selection: Binding(get: { mode ?? "" }, set: { new in change(mode: new) })) {
                ForEach(SessionModes.switchableModes + (mode.flatMap { m in SessionModes.switchableModes.contains { $0.id == m } ? nil : SessionModes.mode(m) }.map { [$0] } ?? [])) { o in
                    Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id)
                }
            } label: {
                Label("权限模式：\(SessionModes.modeLabel(mode) ?? "未知")", systemImage: SessionModes.mode(mode)?.symbol ?? Theme.Icon.askFirst)
            }
            .pickerStyle(.menu)
        }
    }

    private func change(mode: String? = nil, effort: String? = nil) {
        guard mode != session?.permissionMode || effort != session?.effort else { return }
        Task { await model.setModes(key, permissionMode: mode, effort: effort) }
    }
}

/// The model menu's warning while everything is allowed (全部放行): red (a destructive item: its words and its shield),
/// as the composer's button used to be. Picking it does nothing; the permission mode is chosen a few rows up.
struct BypassWarning: View {
    var body: some View {
        Button(role: .destructive) {} label: {
            Label {
                Text("全部放行")
                Text("改文件、跑命令都不问你")
            } icon: {
                // A menu draws its images in the accent unless they carry their own colors.
                Image(systemName: Theme.Icon.allowEverything).symbolRenderingMode(.palette).foregroundStyle(Theme.danger)
            }
        }
        .tint(Theme.danger)
    }
}

/// One tappable chip of `SessionModeBar` / the assistant's starter row.
struct ModeChip: View {
    var symbol: String
    var text: String
    var tint: Color
    var known = true

    var body: some View {
        HStack(spacing: Theme.Space.xs) {
            Image(systemName: symbol).font(Theme.Font.labelStrong)
            Text(text).lineLimit(1)
            Image(systemName: Theme.Icon.choices).font(Theme.Font.labelHeavy).imageScale(.small).opacity(0.6)
        }
        .font(Theme.Font.labelMedium)
        .foregroundStyle(known ? (tint == .secondary ? Color.primary : tint) : Color.secondary)
        .padding(.horizontal, Theme.Space.m)
        .padding(.vertical, Theme.Space.sm)
        .background((known && tint != .secondary ? tint : Color.primary).opacity(known && tint != .secondary ? 0.12 : 0.06), in: .capsule)
        .contentShape(.capsule)
    }
}

/// Mode and effort for something not started yet (a new agent or assistant conversation): "" keeps the launcher's own.
struct NewSessionModePicker: View {
    @Binding var mode: String
    @Binding var effort: String
    var modes: [SessionModes.Option] = SessionModes.permissionModes

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Menu {
                Picker("权限模式", selection: $mode) {
                    Text("跟随启动器").tag("")
                    ForEach(modes) { o in Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id) }
                }
                .pickerStyle(.inline)
            } label: {
                ModeChip(symbol: SessionModes.mode(mode)?.symbol ?? Theme.Icon.askFirst,
                         text: SessionModes.mode(mode)?.title ?? "权限：默认", tint: SessionModes.modeTint(mode), known: !mode.isEmpty)
            }
            .labelOnlyMenu()
            Menu {
                Picker("思考强度", selection: $effort) {
                    Text("跟随启动器").tag("")
                    ForEach(SessionModes.effortLevels) { o in Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id) }
                }
                .pickerStyle(.inline)
            } label: {
                ModeChip(symbol: SessionModes.effort(effort)?.symbol ?? Theme.Icon.effort,
                         text: SessionModes.effort(effort).map { "思考\($0.title)" } ?? "思考：默认", tint: .secondary, known: !effort.isEmpty)
            }
            .labelOnlyMenu()
        }
    }
}
