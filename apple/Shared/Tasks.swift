import Foundation

// Tasks dispatched from an assistant conversation (docs/unified-entry.md, #930): a plan the assistant handed the user,
// the todos it opened (one per task), and the items the hub inserted into the conversation (its feed). The hub's
// shapes are in docs/protocol.md「任务和对话动态」.

/// One task of a plan, as the assistant proposed it (and the user may have edited it).
nonisolated struct PlanTask: Codable, Hashable, Sendable {
    var title: String
    var prompt: String
    var machine: String
    var cwd: String
    var launcher: String?
    var model: String?
    var permission: String?
    var readonly: Bool?
    var why: String?
    /// Set once started: the todo the task is, and why its agent did not start.
    var todoID: String?
    var error: String?

    enum CodingKeys: String, CodingKey {
        case title, prompt, machine, cwd, launcher, model, permission, readonly, why, error
        case todoID = "todo_id"
    }

    var isReadOnly: Bool { readonly ?? false }
    /// The directory's last part, which is how the user names a project.
    var project: String { (cwd as NSString).lastPathComponent }
}

nonisolated struct TaskPlan: Codable, Hashable, Identifiable, Sendable {
    enum State: String, Codable, Sendable {
        case open, started, dismissed, unknown
        init(from decoder: Decoder) throws {
            self = State(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .unknown
        }
    }

    var id: String
    var conversation: String
    var tasks: [PlanTask]
    var state: State
    var createdAt: Date?
    var updatedAt: Date?

    enum CodingKeys: String, CodingKey {
        case id, conversation, tasks, state
        case createdAt = "created_at", updatedAt = "updated_at"
    }
}

/// An item the hub inserted into an assistant conversation; drawn among the transcript's lines by `at`.
nonisolated struct FeedItem: Codable, Hashable, Identifiable, Sendable {
    enum Kind: String, Codable, Sendable {
        case plan, task, answered, receipt, routed, today, message, unknown
        init(from decoder: Decoder) throws {
            self = Kind(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .unknown
        }
    }

    var id: String
    var conversation: String
    var at: Date
    var kind: Kind
    /// The plan id (`plan`) or the todo id (everything else).
    var ref: String?
    var body: JSONValue?

    enum CodingKeys: String, CodingKey { case id, conversation, at, kind, ref, body }

    private func string(_ key: String) -> String? {
        guard case .object(let o)? = body, case .string(let s)? = o[key] else { return nil }
        return s
    }

    /// `answered`: what the card asked and the user's answer.
    var question: String? { string("question") }
    var answer: String? { string("answer") }
    /// `receipt`: the task's title, done / failed, the agent's last words, its PR and machine.
    var title: String? { string("title") }
    var failed: Bool { string("state") == "failed" }
    var summary: String? { string("summary") }
    var pr: String? { string("pr") }
    var machine: String? { string("machine") }
    /// `routed`: what the user said straight to the task; `message`: the message itself.
    var text: String? { string("text") }
    /// `message`: a message about a task with the AI 分身's draft reply; `ref` is the inbox item.
    var todoID: String? { string("todo_id") }
    var source: String? { string("source") }
    var chat: String? { string("chat") }
    var sender: String? { string("sender") }
    var draft: String? { string("draft") }

    /// `today`: the day's card (TodayBody).
    var today: TodayBody? {
        guard kind == .today, let body, let data = try? JSON.encoder.encode(body) else { return nil }
        return try? JSON.decoder.decode(TodayBody.self, from: data)
    }

    /// "PR #901" from a pull request URL.
    var prLabel: String? {
        guard let pr, let n = pr.split(separator: "/").last, Int(n) != nil else { return nil }
        return "PR #\(n)"
    }
}

/// The 今天 card's content (docs/protocol.md「任务和对话动态」, kind `today`), built by the hub.
nonisolated struct TodayBody: Codable, Hashable, Sendable {
    nonisolated struct TaskRef: Codable, Hashable, Sendable { var id: String; var title: String }
    nonisolated struct Meeting: Codable, Hashable, Sendable { var start: Date; var end: Date; var title: String }
    nonisolated struct Chat: Codable, Hashable, Sendable { var name: String; var unread: Int?; var preview: String? }

    var date: String
    var done: [TaskRef]?
    var waiting: [TaskRef]?
    var working: Int?
    var meetings: [Meeting]?
    var atMe: [Chat]?
    var unread: Int?

    enum CodingKeys: String, CodingKey {
        case date, done, waiting, working, meetings, unread
        case atMe = "at_me"
    }
}

/// A todo (docs/todos.md); a task is one whose `origin` is an assistant conversation.
nonisolated struct TodoItem: Codable, Hashable, Identifiable, Sendable {
    nonisolated struct Agent: Codable, Hashable, Sendable {
        var machineID: String
        var sessionID: String
        var title: String?
        var cwd: String?
        var agent: String?
        var startError: String?
        var releasedAt: Date?
        var linkedAt: Date?

        enum CodingKeys: String, CodingKey {
            case title, cwd, agent
            case machineID = "machine_id", sessionID = "session_id", startError = "start_error",
                 releasedAt = "released_at", linkedAt = "linked_at"
        }

        var isActive: Bool { releasedAt == nil }
    }

    var id: String
    var title: String
    var notes: String?
    var doneAt: Date?
    var createdAt: Date?
    var updatedAt: Date?
    var origin: String?
    var planID: String?
    /// The task's agent did not start, and why: the hub ended the todo there (it is done, #1077).
    var startError: String?
    var agents: [Agent]

    enum CodingKeys: String, CodingKey {
        case id, title, notes, origin, agents
        case doneAt = "done_at", createdAt = "created_at", updatedAt = "updated_at", planID = "plan_id", startError = "start_error"
    }

    init(id: String, title: String, notes: String? = nil, doneAt: Date? = nil, createdAt: Date? = nil, updatedAt: Date? = nil,
         origin: String? = nil, planID: String? = nil, startError: String? = nil, agents: [Agent] = []) {
        self.id = id
        self.title = title
        self.notes = notes
        self.doneAt = doneAt
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.origin = origin
        self.planID = planID
        self.startError = startError
        self.agents = agents
    }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        notes = try c.decodeIfPresent(String.self, forKey: .notes)
        doneAt = try c.decodeIfPresent(Date.self, forKey: .doneAt)
        createdAt = try c.decodeIfPresent(Date.self, forKey: .createdAt)
        updatedAt = try c.decodeIfPresent(Date.self, forKey: .updatedAt)
        origin = try c.decodeIfPresent(String.self, forKey: .origin)
        planID = try c.decodeIfPresent(String.self, forKey: .planID)
        startError = try c.decodeIfPresent(String.self, forKey: .startError)
        agents = try c.decodeIfPresent([Agent].self, forKey: .agents) ?? []
    }

    var activeAgents: [Agent] { agents.filter(\.isActive) }
    /// The hub ended it because its agent did not start.
    var failedToStart: Bool { !(startError ?? "").isEmpty }
}

/// Where a task is, from its todo and its agents' states (docs/todos.md「状态计算规则」, the hub's taskState).
nonisolated enum TaskState: String, Sendable, CaseIterable {
    case needsYou, working, unread, followup, notStarted, done, failed

    init(todo: TodoItem, agents: [AgentState]) {
        if todo.failedToStart { self = .failed; return }
        if todo.doneAt != nil { self = .done; return }
        let has = Set(agents)
        if has.contains(.needsYou) { self = .needsYou }
        else if has.contains(.unread) { self = .unread }
        else if has.contains(.working) { self = .working }
        else if !agents.isEmpty { self = .followup }
        else { self = .notStarted }
    }

    /// The agent state whose tone and mark the task borrows.
    var agentState: AgentState {
        switch self {
        case .needsYou: .needsYou
        case .working, .notStarted: .working
        case .unread, .done: .unread
        case .followup, .failed: .idle
        }
    }

    /// Lower sorts first in the dock and the board: what waits for the user, then what runs, then the rest.
    var priority: Int {
        switch self {
        case .needsYou: 0
        case .working, .notStarted: 1
        case .unread: 2
        case .followup: 3
        case .done, .failed: 4
        }
    }

    /// The task finished its work (its last turn ended and nothing waits), or never began it.
    var isSettled: Bool { self == .unread || self == .followup || self == .done || self == .failed }

    /// Nothing of the task runs (it ended, stopped, or its agent never started; `notStarted` stays when the hub never
    /// said how the start went): the user may take it out of the conversation.
    var isRemovable: Bool { self != .working && self != .needsYou }
}

// MARK: - Endpoints

nonisolated extension APIClient {
    struct FeedResponse: Decodable, Sendable {
        var items: [FeedItem]
        var plans: [String: TaskPlan]?
    }

    private struct PlanResponse: Decodable { var plan: TaskPlan }
    private struct TodosResponse: Decodable { var todos: [TodoItem] }
    private struct ItemResponse: Decodable { var item: FeedItem }

    /// A conversation's feed after `since` (nil: all of it), with the plans its `plan` items name.
    func feed(conversation: String, since: Date? = nil) async throws -> FeedResponse {
        let id = conversation.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? conversation
        var query: [String: String] = [:]
        if let since { query["since"] = Self.feedTime.string(from: since) }
        return try await get("/api/v1/assistant/conversations/\(id)/feed", query: query)
    }

    func todos() async throws -> [TodoItem] {
        try await get("/api/v1/todos", as: TodosResponse.self).todos
    }

    /// Starts an open plan; `edits` replace the fields the user changed (by index), `remember` lets this kind start
    /// without asking next time.
    func startPlan(_ id: String, edits: [PlanTask]? = nil, remember: Bool = false) async throws -> TaskPlan {
        struct Body: Encodable { var tasks: [PlanTask]?; var remember: Bool }
        return try await post("/api/v1/task-plans/\(id)/start", body: Body(tasks: edits, remember: remember), as: PlanResponse.self).plan
    }

    func dismissPlan(_ id: String) async throws -> TaskPlan {
        try await post("/api/v1/task-plans/\(id)/dismiss", as: PlanResponse.self).plan
    }

    /// The conversation's 今天 card for the user's day (made on the first call of the day).
    func today(conversation: String) async throws -> FeedItem {
        let id = conversation.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? conversation
        return try await post("/api/v1/assistant/conversations/\(id)/today", as: ItemResponse.self).item
    }

    struct TrustKind: Decodable, Hashable, Sendable {
        var key: String
        var machine: String
        var cwd: String
        var readonly: Bool
    }
    private struct TrustResponse: Decodable { var kinds: [TrustKind] }

    /// The kinds of tasks remembered with 这类以后直接开.
    func taskTrust() async throws -> [TrustKind] { try await get("/api/v1/task-trust", as: TrustResponse.self).kinds }

    func forgetTrust(_ key: String) async throws {
        _ = try await request("DELETE", "/api/v1/task-trust", query: ["key": key])
    }

    /// Sends text straight to a task's agent; the answer is the conversation's new `routed` item.
    func route(conversation: String, todoID: String, text: String) async throws -> FeedItem {
        struct Body: Encodable {
            var todoID: String
            var text: String
            enum CodingKeys: String, CodingKey { case text, todoID = "todo_id" }
        }
        let id = conversation.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? conversation
        return try await post("/api/v1/assistant/conversations/\(id)/route", body: Body(todoID: todoID, text: text), as: ItemResponse.self).item
    }

    nonisolated(unsafe) private static let feedTime: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
}
