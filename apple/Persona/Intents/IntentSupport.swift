import AppIntents
import Foundation

// Siri / Shortcuts entry points. Every intent talks to the hubs directly (HubStore + APIClient), so the app does not
// have to be in the foreground; only OpenSessionIntent runs inside the app and moves its navigation. With several
// groups of hubs (docs/multi-hub.md「Siri」) they see every group: machines and sessions carry their group, the status
// adds the groups up, and whatever acts goes to the group of what it acts on.

/// Everything the intents say out loud. Pure functions, so the tests can pin the wording without a hub.
nonisolated enum IntentText {
    /// Keys the new-chat screen already writes (see NewChatView's AppStorage).
    static let machineKey = "newChat.machine"
    static func launcherKey(_ machine: String) -> String { "launcher.\(machine)" }
    static func cwdKey(_ machine: String) -> String { "cwd.\(machine)" }
    /// The hub group the app has selected (`AppModel.hubGroupKey`): where the todos page reads and writes.
    static let hubGroupKey = "hub.group"

    static func sent(to title: String) -> String {
        let name = title.trimmingCharacters(in: .whitespacesAndNewlines)
        // The untitled case is its own sentence, so 「会话」 is not translated as a bare noun.
        return name.isEmpty ? String(localized: "已发给 会话") : String(localized: "已发给 \(name)")
    }

    static func status(working: Int, waiting: Int, first: String?) -> String {
        let head = String(localized: "\(working) 个在工作，\(waiting) 个等你处理")
        guard let first, !first.isEmpty else { return head }
        return String(localized: "\(head)：\n「\(excerpt(first, limit: 80))」")
    }

    /// One line describing what is waiting, for the status dialog.
    static func waitingPrompt(_ ix: Interaction) -> String {
        switch ix.kind {
        case .question:
            if let q = ix.questions?.first?.question, !q.isEmpty { return q }
            return ix.summary ?? String(localized: "有个提问在等你")
        case .plan:
            // Plans are markdown documents; a dialog needs one line.
            if let plan = ix.plan ?? ix.summary, !plan.isEmpty { return String(localized: "计划：\(excerpt(plan, limit: 120))") }
            return String(localized: "有个计划在等你批准")
        case .permission:
            if let s = ix.summary, !s.isEmpty { return s }
            if let tool = ix.toolName, !tool.isEmpty { return String(localized: "\(tool) 在申请授权") }
            return String(localized: "有个授权在等你")
        case .screen:
            return String(localized: "终端在等你输入")
        }
    }

    static func approvedPermission(_ tool: String?) -> String {
        guard let tool, !tool.isEmpty else { return String(localized: "已允许") }
        return String(localized: "已允许 \(tool)")
    }

    static let approvedPlan = String(localized: "已批准计划，开始执行")

    /// Only permission and plan can be settled without looking at the screen.
    static func needsApp(_ kind: InteractionKind) -> String {
        switch kind {
        case .question: String(localized: "这是个提问，需要打开 Persona 回答。")
        case .screen: String(localized: "终端在等你输入，需要打开 Persona。")
        case .plan, .permission: String(localized: "需要打开 Persona 处理。")
        }
    }

    /// Collapses whitespace and clips, so a plan or a question fits in a Siri dialog.
    static func excerpt(_ text: String, limit: Int = 60) -> String {
        let flat = text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
        guard flat.count > limit else { return flat }
        return String(flat.prefix(limit)) + "…"
    }
}

/// Shortcut wording. The provider spells the same sentences out as `AppShortcutPhrase` interpolations (the system
/// needs the `\(.applicationName)` token there); these templates keep the readable version testable.
nonisolated enum IntentPhrases {
    static let askClaude = "用 %@ 问 Claude" // i18n-ignore
    static let status = "%@ 状态" // i18n-ignore
    static let approve = "用 %@ 批准第一个" // i18n-ignore
    static let addTodo = "用 %@ 新建待办" // i18n-ignore

    static func fill(_ template: String, appName: String) -> String {
        template.replacingOccurrences(of: "%@", with: appName)
    }
}

/// Failures an intent can explain in one Chinese sentence.
nonisolated enum IntentFailure: LocalizedError {
    case notPaired
    case noMachine
    case offline(String)
    case noSession(String)
    case noLauncher(String)
    case noPending

    var errorDescription: String? { sentence }

    var sentence: String {
        switch self {
        case .notPaired:
            String(localized: "还没配对。先打开 Persona，扫 hub 给出的二维码。")
        case .noMachine:
            String(localized: "现在没有在线的电脑。看看你的电脑是不是开着、Persona Link 在不在运行。")
        case .offline(let name):
            String(localized: "\(name) 现在离线，可能睡着了，或者上面的 Persona Link 没在运行。")
        case .noSession(let name):
            String(localized: "\(name) 上没有运行中的会话。打开 Persona 新建一个，或者说「新会话」。")
        case .noLauncher(let name):
            String(localized: "还没在 \(name) 上用过新对话。打开 Persona 新建一次，之后 Siri 就知道用哪个目录和启动器了。")
        case .noPending:
            String(localized: "没有等你处理的事。")
        }
    }

    /// Maps anything thrown while talking to the hub onto a sentence Siri can read out.
    static func sentence(for error: Error, host: String? = nil) -> String {
        if let failure = error as? IntentFailure { return failure.sentence }
        if let api = error as? APIError {
            switch api.status {
            case 401, 403: return String(localized: "配对已失效，打开 Persona 重新配对。")
            case 410: return String(localized: "这个已经处理过了。")
            default: return api.message
            }
        }
        let whereHub = host.map { String(localized: "（\($0)）", comment: "Hub host appended to a connection error") } ?? ""
        if let url = error as? URLError {
            switch url.code {
            case .timedOut: return String(localized: "hub 没响应\(whereHub)，稍后再试。")
            case .notConnectedToInternet, .networkConnectionLost: return String(localized: "手机没网\(whereHub)。")
            default: return String(localized: "连不上 hub\(whereHub)，确认手机能访问 hub（同一个网络或 VPN）。")
            }
        }
        if (error as NSError).domain == NSURLErrorDomain {
            return String(localized: "连不上 hub\(whereHub)，确认手机能访问 hub（同一个网络或 VPN）。")
        }
        return error.localizedDescription
    }
}

nonisolated enum IntentSupport {
    /// Fresh clients every call, one per group (its first hub): the pairing can change while the app is running.
    static func clients() throws -> [APIClient] {
        let registry = HubStore.load()
        let out = registry.groups.compactMap { g in registry.primary(g).map { APIClient(config: $0.config, group: g) } }
        guard !out.isEmpty else { throw IntentFailure.notPaired }
        return out
    }

    /// The client of one group (a machine's, a session's); the first group's when it is gone.
    static func client(group: String = "") throws -> APIClient {
        let all = try clients()
        return all.first { $0.group == group } ?? all[0]
    }

    /// Every group's machines; with several groups each named with its group ("MacBook · Work").
    static func machines() async throws -> [Machine] {
        let all = try clients()
        let registry = HubStore.load()
        var out: [Machine] = []
        for api in all {
            let list = try await api.machines()
            out += all.count > 1 ? list.map { var m = $0; m.groupLabel = registry.name(api.group); return m } : list
        }
        return out
    }

    /// Every group's sessions, or one group's.
    static func sessions(group: String? = nil) async throws -> [Session] {
        var out: [Session] = []
        for api in try clients() where group == nil || api.group == group { out += try await api.sessions() }
        return out
    }

    /// Every group's pending interactions.
    static func interactions() async throws -> [Interaction] {
        var out: [Interaction] = []
        for api in try clients() { out += try await api.interactions() }
        return out
    }

    static func host() -> String? { HubStore.load().hubs.first?.config.url.host() }

    /// A dialog built from runtime text; `IntentDialog`'s literal initializer only takes compile-time strings.
    static func dialog(_ text: String) -> IntentDialog { IntentDialog("\(text)") }
}

/// Selection rules shared by the intents. Pure, so the tests can drive them with decoded models.
nonisolated enum IntentPicker {
    /// The remembered machine if it still exists, otherwise the first online one. `preferred` is what the new-chat
    /// sheet remembers: a machine id, or "group|id" for another group (`MachineRef.path`).
    static func machine(_ machines: [Machine], preferred: String?) -> Machine? {
        if let preferred, let match = machines.first(where: { $0.ref.path == preferred }) { return match }
        return machines.first(where: \.online) ?? machines.first
    }

    /// The most recently updated session on a machine that is not offline.
    static func session(_ sessions: [Session], machine: String, group: String = "") -> Session? {
        sessions
            .filter { $0.machineID == machine && $0.group == group && $0.status != .offline }
            .max { ($0.updatedAt ?? .distantPast) < ($1.updatedAt ?? .distantPast) }
    }

    /// Oldest first: the one that has been waiting longest.
    static func pending(_ interactions: [Interaction]) -> Interaction? {
        interactions.min { ($0.createdAt ?? .distantPast) < ($1.createdAt ?? .distantPast) }
    }
}
