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

    /// `answered`: what the card asked and the user's answer, a line each (the collapsed row).
    var question: String? { string("question") }
    var answer: String? { string("answer") }

    /// `answered`: every question of the card with the user's answer in full, newlines kept (#1231). A record from
    /// before it has only the line, which stands in; empty when there is nothing to show.
    var answeredItems: [AnsweredItem] {
        if case .object(let o)? = body, case .array(let list)? = o["items"] {
            let items = list.compactMap { v -> AnsweredItem? in
                guard case .object(let it) = v else { return nil }
                let item = AnsweredItem(question: it["question"]?.string ?? "", answer: it["answer"]?.string ?? "")
                return item.question.isEmpty && item.answer.isEmpty ? nil : item
            }
            if !items.isEmpty { return items }
        }
        let line = AnsweredItem(question: question ?? "", answer: answer ?? "")
        return line.question.isEmpty && line.answer.isEmpty ? [] : [line]
    }
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

/// One question of an answered card and what the user answered (`FeedItem.answeredItems`).
nonisolated struct AnsweredItem: Hashable, Sendable {
    var question: String
    var answer: String
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
    nonisolated struct Space: Codable, Hashable, Sendable {
        var machineID: String
        var space: String
        var linkedAt: Date?

        enum CodingKeys: String, CodingKey {
            case space
            case machineID = "machine_id", linkedAt = "linked_at"
        }
    }

    nonisolated struct Agent: Codable, Hashable, Sendable {
        var machineID: String
        var sessionID: String
        var title: String?
        var cwd: String?
        var agent: String?
        var startError: String?
        var releasedAt: Date?
        var linkedAt: Date?
        var source: String?
        var via: String?

        enum CodingKeys: String, CodingKey {
            case title, cwd, agent, source, via
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
    /// "auto" when the hub marked the task done because its work was judged finished, with why (#1092,
    /// docs/todos.md「自动完成」); nil when the user did.
    var doneBy: String?
    var doneReason: String?
    var agents: [Agent]
    var spaces: [Space]?

    enum CodingKeys: String, CodingKey {
        case id, title, notes, origin, agents, spaces
        case doneAt = "done_at", createdAt = "created_at", updatedAt = "updated_at", planID = "plan_id", startError = "start_error",
             doneBy = "done_by", doneReason = "done_reason"
    }

    init(id: String, title: String, notes: String? = nil, doneAt: Date? = nil, createdAt: Date? = nil, updatedAt: Date? = nil,
         origin: String? = nil, planID: String? = nil, startError: String? = nil, doneBy: String? = nil, doneReason: String? = nil,
         agents: [Agent] = [], spaces: [Space]? = nil) {
        self.id = id
        self.title = title
        self.notes = notes
        self.doneAt = doneAt
        self.createdAt = createdAt
        self.updatedAt = updatedAt
        self.origin = origin
        self.planID = planID
        self.startError = startError
        self.doneBy = doneBy
        self.doneReason = doneReason
        self.agents = agents
        self.spaces = spaces
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
        doneBy = try c.decodeIfPresent(String.self, forKey: .doneBy)
        doneReason = try c.decodeIfPresent(String.self, forKey: .doneReason)
        agents = try c.decodeIfPresent([Agent].self, forKey: .agents) ?? []
        spaces = try c.decodeIfPresent([Space].self, forKey: .spaces)
    }

    var activeAgents: [Agent] { agents.filter(\.isActive) }
    /// The hub ended it because its agent did not start.
    var failedToStart: Bool { !(startError ?? "").isEmpty }
    /// The hub marked it done because its task's work was judged finished; reopening undoes it.
    var autoDone: Bool { doneAt != nil && doneBy == "auto" }
}

/// An agent going on (等你 / 在做) as the main conversation's dock shows it after the tasks (#1113): the ones the user
/// opened on the Agents page, which no conversation's task stands for. Made from the Agents page's session list, so
/// it follows the agents without a poll of its own.
nonisolated struct DockAgent: Hashable, Identifiable, Sendable {
    var key: SessionKey
    var title: String
    var state: AgentState
    /// When it entered its state (the hub's `state_since`): the newest first among the same state.
    var since: Date?
    /// The todo it works for, as the hub tags the session (a task's, or one made by hand).
    var todoID: String?

    var id: SessionKey { key }
    var isWaiting: Bool { state == .needsYou }

    init(key: SessionKey, title: String, state: AgentState, since: Date? = nil, todoID: String? = nil) {
        self.key = key
        self.title = title
        self.state = state
        self.since = since
        self.todoID = todoID
    }

    /// The agents going on in a session list (Persona's own conversations are not agents): waiting for the user
    /// first, then the newest. Only what a pill shows goes in, so it changes when a pill would.
    @MainActor static func going(_ sessions: [Session]) -> [DockAgent] {
        sessions.filter { !$0.assistant && ($0.state == .needsYou || $0.state == .working) }
            .map { DockAgent(key: $0.key, title: $0.displayTitle, state: $0.state, since: $0.stateSince, todoID: $0.todo?.id) }
            .sorted { a, b in
                if a.state.priority != b.state.priority { return a.state.priority < b.state.priority }
                if a.since != b.since { return (a.since ?? .distantPast) > (b.since ?? .distantPast) }
                return a.key.path < b.key.path
            }
    }

    /// What a conversation's dock shows after its tasks: in the main conversation, every agent going on that no task
    /// stands for (a task's agents are its pill already, whichever conversation opened it); elsewhere nothing.
    /// `taskAgents` are the agents of the tasks not done. An agent the hub tags with a todo shows only when that todo
    /// is one of `manualTodos` (made by hand): a task the todo list has not caught up with yet stays out too. Those
    /// are the todos of `group`'s hub; another group's agent tagged with a todo of its own hub stays out.
    static func shown(_ going: [DockAgent], conversation: String, main: String?, group: String,
                      taskAgents: Set<SessionKey>, manualTodos: Set<String>) -> [DockAgent] {
        guard let main, !main.isEmpty, conversation == main else { return [] }
        return going.filter { a in
            guard !taskAgents.contains(a.key) else { return false }
            guard let todo = a.todoID else { return true }
            return a.key.group == group && manualTodos.contains(todo)
        }
    }
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

/// What a task's card says its agent is on now (docs/unified-entry.md「任务卡片的进度」): its latest words, or — while it
/// only runs tools (reads code, runs tests, waits for CI) and its words stay put for minutes — the step it is on and
/// since when. New words win again as soon as they come (the hub drops the step then).
nonisolated enum TaskNow: Equatable, Sendable {
    case words(String)
    case step(String, since: Date?)

    /// From the task's lead agent; nil when it has said and done nothing yet. A working agent's step comes before its
    /// words (the hub only sends a step newer than them); once it stopped, its words do (a turn cut short on a tool call).
    init?(lead: Session?, working: Bool) {
        let words = lead?.lastText?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let step = lead?.activity?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        if !step.isEmpty, working || words.isEmpty {
            self = .step(step, since: working ? lead?.activityAt : nil)
        } else if !words.isEmpty {
            self = .words(words)
        } else {
            return nil
        }
    }

    var text: String {
        switch self {
        case .words(let s), .step(let s, _): s
        }
    }
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
