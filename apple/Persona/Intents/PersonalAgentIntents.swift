import AppIntents
import CoreSpotlight
import Foundation

// Personal agents (/api/v1/agents/*) for Siri, Shortcuts and Spotlight: pick a session, hand it a task, hear what is
// unread. Everything goes straight to the hub, like the other intents.

/// A personal-agent session as Siri sees it. Indexed in Spotlight when the Agents page loads (`PersonalAgentIndex`).
struct PersonalAgentEntity: IndexedEntity, Hashable, Sendable {
    nonisolated static let typeDisplayRepresentation = TypeDisplayRepresentation(name: "个人 Agent")
    nonisolated static let defaultQuery = PersonalAgentEntityQuery()

    /// The hub's session id, "<source>:<sessionKey>".
    var id: String
    var title: String
    var provider: String
    var unread: Int
    var canSend: Bool

    nonisolated init(id: String, title: String, provider: String = "", unread: Int = 0, canSend: Bool = true) {
        self.id = id
        self.title = title
        self.provider = provider
        self.unread = unread
        self.canSend = canSend
    }

    nonisolated init(_ s: PersonalAgentSession) {
        self.init(id: s.id, title: s.title, provider: s.provider, unread: s.unreadCount, canSend: s.canSend)
    }

    var displayRepresentation: DisplayRepresentation {
        DisplayRepresentation(title: "\(PersonalAgentIntentText.name(title: title, id: id))",
                              subtitle: "\(PersonalAgentIntentText.subtitle(provider: provider, unread: unread))")
    }
}

struct PersonalAgentEntityQuery: EntityQuery, EntityStringQuery {
    nonisolated init() {}

    nonisolated func entities(for identifiers: [String]) async throws -> [PersonalAgentEntity] {
        let all = try await Self.all()
        return identifiers.map { id in all.first { $0.id == id } ?? PersonalAgentEntity(id: id, title: id) }
    }

    nonisolated func suggestedEntities() async throws -> [PersonalAgentEntity] {
        PersonalAgentIntentPicker.suggested(try await Self.all())
    }

    nonisolated func entities(matching string: String) async throws -> [PersonalAgentEntity] {
        PersonalAgentIntentPicker.matching(string, in: try await Self.all())
    }

    private static func all() async throws -> [PersonalAgentEntity] {
        do {
            return try await IntentSupport.client().personalAgentSessions().map(PersonalAgentEntity.init)
        } catch {
            throw IntentFailureSentence(IntentFailure.sentence(for: error, host: IntentSupport.host()))
        }
    }
}

/// "让 Agent 去做某件事": one message into a personal agent session, with a fresh clientId (the hub dedupes retries).
struct AgentTaskIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "让 Agent 去做"
    nonisolated static let description = IntentDescription(
        "把一件事发给某个个人 Agent 会话（飞书网关的 agent-bridge），由它去做。不选会话时发给最近活跃、能发送的那个。",
        categoryName: "个人 Agent",
        searchKeywords: ["Agent", "个人 Agent", "任务", "让它去做", "task"]
    )
    nonisolated static var supportedModes: IntentModes { .background }

    @Parameter(title: "会话")
    var agent: PersonalAgentEntity?

    @Parameter(title: "要做的事", requestValueDialog: "要让 Agent 做什么？")
    var task: String

    nonisolated init() {}
    nonisolated init(agent: PersonalAgentEntity?, task: String) {
        self.agent = agent
        self.task = task
    }

    nonisolated static var parameterSummary: some ParameterSummary { Summary("让 \(\.$agent) 去做 \(\.$task)") }

    nonisolated func perform() async throws -> some IntentResult & ProvidesDialog & ReturnsValue<String> {
        let body = task.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !body.isEmpty else { throw IntentFailureSentence("要让 Agent 做什么？") }
        do {
            let api = try IntentSupport.client()
            let target: PersonalAgentEntity
            if let agent { target = agent } else {
                let all = try await api.personalAgentSessions().map(PersonalAgentEntity.init)
                guard let first = PersonalAgentIntentPicker.defaultTarget(all) else {
                    throw IntentFailureSentence(PersonalAgentIntentText.noSession)
                }
                target = first
            }
            guard target.canSend else { throw IntentFailureSentence(PersonalAgentIntentText.cannotSend(target.title)) }
            let r = try await api.sendPersonalAgent(target.id, clientId: UUID().uuidString, text: body)
            let text = PersonalAgentIntentText.sent(status: r.outbox.status, to: target.title, error: r.outbox.error)
            return .result(value: text, dialog: IntentSupport.dialog(text))
        } catch let e as IntentFailureSentence {
            throw e
        } catch {
            throw IntentFailureSentence(IntentFailure.sentence(for: error, host: IntentSupport.host()))
        }
    }
}

/// "个人 Agent 有什么新消息": unread counts across personal agent sessions, the busiest first.
struct AgentsStatusIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "个人 Agent 未读"
    nonisolated static let description = IntentDescription(
        "最近的个人 Agent 会话和各自的未读条数。",
        categoryName: "个人 Agent",
        searchKeywords: ["Agent", "未读", "最近", "unread"]
    )
    nonisolated static var supportedModes: IntentModes { .background }

    nonisolated init() {}

    nonisolated func perform() async throws -> some IntentResult & ProvidesDialog & ReturnsValue<String> {
        do {
            let all = try await IntentSupport.client().personalAgentSessions().map(PersonalAgentEntity.init)
            let text = PersonalAgentIntentText.status(all)
            return .result(value: text, dialog: IntentSupport.dialog(text))
        } catch {
            throw IntentFailureSentence(IntentFailure.sentence(for: error, host: IntentSupport.host()))
        }
    }
}

/// Opens the app on the personal agents list (Spotlight results and the dialog buttons land here).
struct OpenPersonalAgentIntent: OpenIntent {
    nonisolated static let title: LocalizedStringResource = "打开个人 Agent"
    nonisolated static var supportedModes: IntentModes { .foreground(.immediate) }

    @Parameter(title: "会话")
    var target: PersonalAgentEntity

    nonisolated init() {}
    nonisolated init(target: PersonalAgentEntity) { self.target = target }

    nonisolated func perform() async throws -> some IntentResult {
        await MainActor.run { AppModel.shared.selectedTab = .chats }
        return .result()
    }
}

/// Pure choices, so the tests pin them without a hub.
nonisolated enum PersonalAgentIntentPicker {
    /// Unread first, then the order the hub gave (most recent first).
    static func suggested(_ all: [PersonalAgentEntity]) -> [PersonalAgentEntity] {
        all.filter { $0.unread > 0 } + all.filter { $0.unread == 0 }
    }

    static func matching(_ needle: String, in all: [PersonalAgentEntity]) -> [PersonalAgentEntity] {
        let n = needle.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !n.isEmpty else { return suggested(all) }
        return all.filter { $0.title.localizedCaseInsensitiveContains(n) || $0.provider.localizedCaseInsensitiveContains(n) }
    }

    /// The hub lists sessions newest first; the first one we may send to.
    static func defaultTarget(_ all: [PersonalAgentEntity]) -> PersonalAgentEntity? { all.first(where: \.canSend) }
}

nonisolated enum PersonalAgentIntentText {
    static let noSession = "还没有能发消息的个人 Agent 会话。先在飞书网关里开一个。"

    static func name(title: String, id: String) -> String {
        let t = title.trimmingCharacters(in: .whitespacesAndNewlines)
        return t.isEmpty ? id : t
    }

    static func subtitle(provider: String, unread: Int) -> String {
        unread > 0 ? "\(provider) · \(unread) 条未读" : provider
    }

    static func cannotSend(_ title: String) -> String { "「\(title)」不能从这里发消息。" }

    static func sent(status: String, to title: String, error: String?) -> String {
        switch status {
        case "failed": "发给「\(title)」失败：\(error ?? "未知错误")"
        case "sending": "正在发给「\(title)」"
        default: "已交给「\(title)」"
        }
    }

    static func status(_ all: [PersonalAgentEntity], limit: Int = 3) -> String {
        guard !all.isEmpty else { return "没有个人 Agent 会话。" }
        let unread = all.filter { $0.unread > 0 }
        guard !unread.isEmpty else {
            return "没有未读。最近的是：" + all.prefix(limit).map { "「\($0.title)」" }.joined(separator: "、")
        }
        let total = unread.reduce(0) { $0 + $1.unread }
        let list = unread.prefix(limit).map { "「\($0.title)」\($0.unread) 条" }.joined(separator: "，")
        return "\(unread.count) 个会话共 \(total) 条未读：\(list)"
    }
}

/// Spotlight: the sessions the Agents page just loaded become searchable (and Siri can suggest them).
nonisolated enum PersonalAgentIndex {
    static func update(_ sessions: [PersonalAgentSession]) {
        let entities = sessions.map(PersonalAgentEntity.init)
        Task.detached(priority: .utility) {
            try? await CSSearchableIndex.default().indexAppEntities(entities)
        }
        JarvisShortcuts.updateAppShortcutParameters()
    }
}
