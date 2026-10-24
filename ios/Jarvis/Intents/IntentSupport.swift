import AppIntents
import Foundation

// Siri / Shortcuts entry points. Every intent talks to the hub directly (Keychain.serverConfig + APIClient), so the
// app does not have to be in the foreground; only OpenSessionIntent runs inside the app and moves its navigation.

/// Everything the intents say out loud. Pure functions, so the tests can pin the wording without a hub.
nonisolated enum IntentText {
    /// Keys the new-chat screen already writes (see NewChatView's AppStorage).
    static let machineKey = "newChat.machine"
    static func launcherKey(_ machine: String) -> String { "launcher.\(machine)" }
    static func cwdKey(_ machine: String) -> String { "cwd.\(machine)" }

    static func sent(to title: String) -> String {
        let name = title.trimmingCharacters(in: .whitespacesAndNewlines)
        return "已发给 \(name.isEmpty ? "会话" : name)"
    }

    static func status(working: Int, waiting: Int, first: String?) -> String {
        let head = "\(working) 个在工作，\(waiting) 个等你处理"
        guard let first, !first.isEmpty else { return head }
        return "\(head)：\n「\(excerpt(first, limit: 80))」"
    }

    /// One line describing what is waiting, for the status dialog.
    static func waitingPrompt(_ ix: Interaction) -> String {
        switch ix.kind {
        case .question:
            if let q = ix.questions?.first?.question, !q.isEmpty { return q }
            return ix.summary ?? "有个提问在等你"
        case .plan:
            // Plans are markdown documents; a dialog needs one line.
            if let plan = ix.plan ?? ix.summary, !plan.isEmpty { return "计划：" + excerpt(plan, limit: 120) }
            return "有个计划在等你批准"
        case .permission:
            if let s = ix.summary, !s.isEmpty { return s }
            if let tool = ix.toolName, !tool.isEmpty { return "\(tool) 在申请授权" }
            return "有个授权在等你"
        case .screen:
            return "终端在等你输入"
        }
    }

    static func approvedPermission(_ tool: String?) -> String {
        guard let tool, !tool.isEmpty else { return "已允许" }
        return "已允许 \(tool)"
    }

    static let approvedPlan = "已批准计划，开始执行"

    /// Only permission and plan can be settled without looking at the screen.
    static func needsApp(_ kind: InteractionKind) -> String {
        switch kind {
        case .question: "这是个提问，需要打开 Jarvis 回答。"
        case .screen: "终端在等你输入，需要打开 Jarvis。"
        case .plan, .permission: "需要打开 Jarvis 处理。"
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
    static let askClaude = "用 %@ 问 Claude"
    static let status = "%@ 状态"
    static let approve = "用 %@ 批准第一个"

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
            "还没配对。先打开 Jarvis 扫 `jarvisd hub pair` 的二维码。"
        case .noMachine:
            "现在没有在线的机器。看看 example-machine 上的 jarvisd 和 SSH 隧道。"
        case .offline(let name):
            "\(name) 现在离线，看看 jarvisd 和 SSH 隧道。"
        case .noSession(let name):
            "\(name) 上没有运行中的会话。打开 Jarvis 新建一个，或者说「新会话」。"
        case .noLauncher(let name):
            "还没在 \(name) 上用过新对话。打开 Jarvis 新建一次，之后 Siri 就知道用哪个目录和启动器了。"
        case .noPending:
            "没有等你处理的事。"
        }
    }

    /// Maps anything thrown while talking to the hub onto a sentence Siri can read out.
    static func sentence(for error: Error, host: String? = nil) -> String {
        if let failure = error as? IntentFailure { return failure.sentence }
        if let api = error as? APIError {
            switch api.status {
            case 401, 403: return "配对已失效，打开 Jarvis 重新配对。"
            case 410: return "这个已经处理过了。"
            default: return api.message
            }
        }
        let whereHub = host.map { "（\($0)）" } ?? ""
        if let url = error as? URLError {
            switch url.code {
            case .timedOut: return "hub 没响应\(whereHub)，稍后再试。"
            case .notConnectedToInternet, .networkConnectionLost: return "手机没网\(whereHub)。"
            default: return "连不上 hub\(whereHub)，手机要在公司网络或 removed 上。"
            }
        }
        if (error as NSError).domain == NSURLErrorDomain {
            return "连不上 hub\(whereHub)，手机要在公司网络或 removed 上。"
        }
        return error.localizedDescription
    }
}

nonisolated enum IntentSupport {
    /// Fresh client every call: the pairing can change while the app is running.
    static func client() throws -> APIClient {
        guard let config = Keychain.serverConfig else { throw IntentFailure.notPaired }
        return APIClient(config: config)
    }

    static func host() -> String? { Keychain.serverConfig?.url.host() }

    /// A dialog built from runtime text; `IntentDialog`'s literal initializer only takes compile-time strings.
    static func dialog(_ text: String) -> IntentDialog { IntentDialog("\(text)") }
}

/// Selection rules shared by the intents. Pure, so the tests can drive them with decoded models.
nonisolated enum IntentPicker {
    /// The remembered machine if it still exists, otherwise the first online one.
    static func machine(_ machines: [Machine], preferred: String?) -> Machine? {
        if let preferred, let match = machines.first(where: { $0.id == preferred }) { return match }
        return machines.first(where: \.online) ?? machines.first
    }

    /// The most recently updated session on a machine that is not offline.
    static func session(_ sessions: [Session], machine: String) -> Session? {
        sessions
            .filter { $0.machineID == machine && $0.status != .offline }
            .max { ($0.updatedAt ?? .distantPast) < ($1.updatedAt ?? .distantPast) }
    }

    /// Oldest first: the one that has been waiting longest.
    static func pending(_ interactions: [Interaction]) -> Interaction? {
        interactions.min { ($0.createdAt ?? .distantPast) < ($1.createdAt ?? .distantPast) }
    }
}
