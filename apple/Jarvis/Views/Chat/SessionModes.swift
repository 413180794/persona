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

    /// Modes a running session can switch to (bypassPermissions only at start, see node/modes.go).
    static let switchableModes = permissionModes.filter { $0.id != "bypassPermissions" }

    static func mode(_ id: String?) -> Option? { permissionModes.first { $0.id == id } }
    static func effort(_ id: String?) -> Option? { effortLevels.first { $0.id == id } }

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
}

/// The two chips above the composer — permission mode and effort level, like the footer of Claude Code's TUI.
/// Tapping one changes it on the running session.
struct SessionModeBar: View {
    @Environment(AppModel.self) private var model
    let key: SessionKey
    var session: Session?
    @State private var changing = false

    var body: some View {
        let mode = session?.permissionMode
        let effort = session?.effort
        HStack(spacing: Theme.Space.s) {
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
            .accessibilityLabel("权限模式：\(SessionModes.modeLabel(mode) ?? "未知")")
            Menu {
                Picker("思考强度", selection: Binding(get: { effort ?? "" }, set: { new in change(effort: new) })) {
                    ForEach(SessionModes.effortLevels) { o in
                        Label { Text(o.title); Text(o.detail) } icon: { Image(systemName: o.symbol) }.tag(o.id)
                    }
                }
                .pickerStyle(.inline)
            } label: {
                ModeChip(symbol: SessionModes.effort(effort)?.symbol ?? Theme.Icon.effort,
                         text: effort.map { "思考\(SessionModes.effortLabel($0) ?? $0)" } ?? "思考强度", tint: .secondary, known: effort != nil)
            }
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
        return list + [SessionModes.mode(current) ?? .init(id: current, title: current, detail: "", symbol: "questionmark.circle")]
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
        }
    }
}
