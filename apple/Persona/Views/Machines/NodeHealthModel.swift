import Foundation
import Observation

// What This Computer Can Do (#3018, the web's #2429, docs/machines.md「自检」): what `personad node doctor` finds on a computer, read
// as features. The node reports individual checks (server/internal/node/doctor.go, DoctorReport as it is); a person
// wants to know whether phone approvals work, so each feature is a set of those checks and takes the worst state among
// them. The grouping, the states and the words are the web's (web/src/lib/node-health.ts): change both together.

/// One line of the node's report; `state` is current / stale / missing for a generated file, "" for the others.
nonisolated struct DoctorCheck: Decodable, Hashable, Sendable {
    enum Status: String, Sendable { case ok, warn, fail }

    var name: String
    var status: Status
    var state: String
    var detail: String
    var hint: String
    var items: [String]

    init(name: String, status: Status, state: String = "", detail: String = "", hint: String = "", items: [String] = []) {
        self.name = name
        self.status = status
        self.state = state
        self.detail = detail
        self.hint = hint
        self.items = items
    }

    private enum CodingKeys: String, CodingKey { case name, status, state, detail, hint, items }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        // A status a newer node adds reads as a warning: shown, not hidden, and not called broken.
        status = Status(rawValue: try c.decodeIfPresent(String.self, forKey: .status) ?? "") ?? .warn
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? ""
        detail = try c.decodeIfPresent(String.self, forKey: .detail) ?? ""
        hint = try c.decodeIfPresent(String.self, forKey: .hint) ?? ""
        items = try c.decodeIfPresent([String].self, forKey: .items) ?? []
    }

    var statusLabel: String {
        switch status {
        case .ok: String(localized: "正常")
        case .warn: String(localized: "注意")
        case .fail: String(localized: "有问题")
        }
    }

    /// Persona's own generated file is out of date or gone: Repair writes it again.
    var fixable: Bool { state == "stale" || state == "missing" }
}

nonisolated struct DoctorReport: Decodable, Sendable {
    var ok: Bool
    var checks: [DoctorCheck]
    /// The generated files `fix` wrote again.
    var fixed: [String]

    init(ok: Bool = true, checks: [DoctorCheck], fixed: [String] = []) {
        self.ok = ok
        self.checks = checks
        self.fixed = fixed
    }

    private enum CodingKeys: String, CodingKey { case ok, checks, fixed }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        ok = try c.decodeIfPresent(Bool.self, forKey: .ok) ?? false
        checks = try c.decodeIfPresent([DoctorCheck].self, forKey: .checks) ?? []
        fixed = try c.decodeIfPresent([String].self, forKey: .fixed) ?? []
    }
}

nonisolated enum NodeHealth {
    enum FeatureState: Sendable {
        case ready, partial, broken, off

        var label: String {
            switch self {
            case .ready: String(localized: "就绪")
            case .partial: String(localized: "部分可用")
            case .broken: String(localized: "有问题")
            case .off: String(localized: "未启用")
            }
        }
    }

    struct FeatureSpec: Sendable {
        var id: String
        var name: String
        /// What it lets the user do, one line.
        var detail: String
        /// The checks it stands on (DoctorCheck.name).
        var checks: [String]
        /// Something a computer may well not use: a warning alone means Off.
        var optional = false
        /// For an optional one: whether this computer needs it (by its engine), which turns the warning into Problem.
        var needed: (@Sendable (MachineEngine) -> Bool)?
    }

    struct Feature: Identifiable, Sendable {
        var id: String
        var name: String
        var detail: String
        var state: FeatureState
        var checks: [DoctorCheck]
        /// Why it is not ready, from its worst check: what to do, else what was found. "" when ready.
        var note: String
    }

    /// The checks the node reports and no feature stands on: the user's own settings, shown as information.
    static let infoCheckNames = ["global-permission-hooks"]

    static let features: [FeatureSpec] = [
        FeatureSpec(id: "approvals", name: String(localized: "手机审批卡片"), detail: String(localized: "在手机上回答提问、批准工具和计划"),
                    checks: ["node.sock", "claude-settings.json", "claude-hooks", "assistant-settings.json", "assistant-hooks", "command-paths"]),
        FeatureSpec(id: "status", name: String(localized: "状态与通知"), detail: String(localized: "Agent 的状态变化和完成通知"), checks: ["node.sock", "command-paths"]),
        FeatureSpec(id: "terminal", name: String(localized: "终端与引擎"), detail: String(localized: "手机上看和操作这台电脑的终端"), checks: ["herdr"], optional: true,
                    needed: { $0 == .herdr || $0 == .herdrBoth }),
        FeatureSpec(id: "mcp", name: String(localized: "MCP 工具"), detail: String(localized: "Agent 里能用 Persona 的工具"), checks: ["node.sock", "mcp.json", "command-paths"]),
        FeatureSpec(id: "skills", name: String(localized: "技能"), detail: String(localized: "Persona 启动的 Agent 自带的技能"), checks: ["agent-plugin", "claude-version"]),
        FeatureSpec(id: "runtimes", name: String(localized: "运行时接入"), detail: String(localized: "DSH、pi 这类 runtime 连到这台电脑"), checks: ["runtime.sock"], optional: true),
    ]

    private static func rank(_ s: DoctorCheck.Status) -> Int { s == .ok ? 0 : s == .warn ? 1 : 2 }

    private static func worst(_ checks: [DoctorCheck]) -> DoctorCheck? {
        var w: DoctorCheck?
        for c in checks where c.status != .ok && (w == nil || rank(c.status) > rank(w!.status)) { w = c }
        return w
    }

    /// A stale or missing generated file has Repair here, so the node's hint (a command line) is not repeated.
    private static func note(_ c: DoctorCheck?) -> String {
        guard let c else { return "" }
        if c.state == "stale" { return String(localized: "Persona 生成的文件过期了，点「修复」重新生成") }
        if c.state == "missing" { return String(localized: "Persona 生成的文件不见了，点「修复」重新生成") }
        return c.hint.isEmpty ? c.detail : c.hint
    }

    /// The report as features, in a fixed order. A feature none of whose checks the report has (an older node reports
    /// fewer) is left out; a check no feature covers and that is not fine makes Other Checks so nothing hides.
    static func derive(_ report: DoctorReport, engine: MachineEngine = .herdr) -> [Feature] {
        var byName: [String: DoctorCheck] = [:]
        for c in report.checks where byName[c.name] == nil { byName[c.name] = c }
        var out: [Feature] = []
        for spec in features {
            let checks = spec.checks.compactMap { byName[$0] }
            if checks.isEmpty { continue }
            let w = worst(checks)
            var state = FeatureState.ready
            if w?.status == .fail {
                state = .broken
            } else if w?.status == .warn {
                state = spec.optional ? (spec.needed?(engine) == true ? .broken : .off) : .partial
            }
            out.append(Feature(id: spec.id, name: spec.name, detail: spec.detail, state: state, checks: checks, note: note(w)))
        }
        let covered = Set(features.flatMap(\.checks) + infoCheckNames)
        let rest = report.checks.filter { !covered.contains($0.name) }
        if let w = worst(rest) {
            out.append(Feature(id: "other", name: String(localized: "其他检查"), detail: String(localized: "这个版本的 node 多报的项"),
                               state: w.status == .fail ? .broken : .partial, checks: rest, note: note(w)))
        }
        return out
    }

    struct Summary: Equatable, Sendable {
        /// Features that are broken or partial.
        var attention: Int
        var title: String
        /// Not set up: … ("" when every feature is in use).
        var off: String
    }

    static func summarize(_ features: [Feature]) -> Summary {
        let attention = features.filter { $0.state == .broken || $0.state == .partial }.count
        let off = features.filter { $0.state == .off }.map(\.name)
        return Summary(attention: attention, title: attention == 0 ? String(localized: "一切正常") : String(localized: "\(attention) 项功能需要处理"),
                       off: off.isEmpty ? "" : String(localized: "没有设置：\(off.joined(separator: "、"))"))
    }

    /// The checks Repair would put right: Persona's own generated files that are stale or missing.
    static func fixable(_ report: DoctorReport) -> [DoctorCheck] { report.checks.filter(\.fixable) }

    /// The user's own settings the node only looks at: a row of information, not a feature.
    static func info(_ report: DoctorReport) -> [DoctorCheck] {
        report.checks.filter { infoCheckNames.contains($0.name) && $0.status != .ok }
    }

    /// What the toast says after Repair: the node re-checks after writing, so the report is already the new one.
    static func fixResult(_ report: DoctorReport) -> (text: String, failed: Bool) {
        if let failed = report.checks.first(where: { $0.name == "fix" && $0.status == .fail }) { return (failed.detail, true) }
        return report.fixed.isEmpty ? (String(localized: "没有需要重新生成的文件"), false) : (String(localized: "已重新生成 \(report.fixed.count) 项"), false)
    }

    enum Unavailable: Equatable, Sendable { case oldNode, oldHub, offline, forbidden }

    /// Why a self-check could not be had, from the hub's answer. An old node answers "unknown method"; an old hub has no
    /// such route (a plain-text 404, not the JSON of "unknown machine").
    static func unavailable(_ error: any Error) -> Unavailable? {
        guard let e = error as? APIError else { return nil }
        if e.message.range(of: "unknown method", options: .caseInsensitive) != nil { return .oldNode }
        if e.status == 404 && e.details == nil { return .oldHub }
        if e.status == 503 { return .offline }
        if e.status == 403 { return .forbidden }
        return nil
    }

    /// Whether the hub can install its own build there (Redeploy): not the hub's own machine, not one that dials in.
    static func canDeploy(_ m: Machine) -> Bool { !["local", "link", "service"].contains(m.kind) }

    /// Why an old node cannot be updated from here, when it cannot.
    static func deployReason(_ m: Machine) -> String? {
        switch m.kind {
        case "local": String(localized: "这是 hub 所在的机器，请在本机升级 hub 和 node。")
        case "link", "service": String(localized: "请先在这台电脑上本地升级 node，并将 hub 升级到支持链路更新的版本。")
        default: nil
        }
    }
}

/// The section's state: checks once when shown (and on Check Again); Repair writes and comes back with the new report.
@MainActor @Observable final class NodeHealthModel {
    struct Deps {
        var check: () async throws -> DoctorReport
        var fix: () async throws -> DoctorReport
    }

    enum Load {
        case idle
        case loading
        case report(DoctorReport)
        case unavailable(NodeHealth.Unavailable)
        case error(String)
    }

    private(set) var load: Load = .idle
    /// A re-check or a fix is running over the report that is shown.
    private(set) var busy = false
    private(set) var fixing = false
    @ObservationIgnored private let deps: Deps
    /// Answers to an older request are dropped (a re-check while a fix is on its way).
    @ObservationIgnored private var seq = 0

    init(deps: Deps) { self.deps = deps }

    var report: DoctorReport? {
        if case .report(let r) = load { return r }
        return nil
    }

    /// An offline computer: nothing to ask, and nothing older to show.
    func reset() {
        seq += 1
        load = .idle
        busy = false
    }

    func check() async {
        seq += 1
        let mine = seq
        if report == nil { load = .loading }
        busy = true
        defer { if mine == seq { busy = false } }
        do {
            let r = try await deps.check()
            guard mine == seq else { return }
            load = .report(r)
        } catch {
            guard mine == seq, !error.isCancellation else { return }
            if let why = NodeHealth.unavailable(error) {
                load = .unavailable(why)
            } else {
                load = .error(error.shown ?? error.localizedDescription)
            }
        }
    }

    /// Returns what to tell the user (a toast), failed when the fix did not go through.
    func fix() async -> (text: String, failed: Bool) {
        seq += 1
        let mine = seq
        fixing = true
        busy = true
        defer {
            fixing = false
            if mine == seq { busy = false }
        }
        do {
            let r = try await deps.fix()
            if mine == seq { load = .report(r) }
            return NodeHealth.fixResult(r)
        } catch {
            return (String(localized: "修复失败：\(error.shown ?? error.localizedDescription)"), true)
        }
    }
}

extension APIClient {
    /// What This Computer Can Do (docs/machines.md「自检」): the node's self-check, read only. Full devices only (403 otherwise);
    /// an old node answers "unknown method", an old hub a plain 404. The hub gives the node 45 s.
    func nodeDoctor(machine: String) async throws -> DoctorReport {
        try JSON.decoder.decode(DoctorReport.self, from: await request("GET", "/api/v1/machines/\(machine)/doctor", timeout: 50))
    }

    /// Repair: the node writes Persona's own generated files again (never the user's settings) and re-checks.
    func fixNodeDoctor(machine: String) async throws -> DoctorReport {
        try await post("/api/v1/machines/\(machine)/doctor/fix", timeout: 65, as: DoctorReport.self)
    }
}
