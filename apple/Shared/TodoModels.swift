import Foundation
import SwiftUI

nonisolated extension TodoItem {
    var isDone: Bool { doneAt != nil }

    /// The state's label for this todo: 「自动完成」 for one the hub closed (docs/todos.md「自动完成」).
    func statusLabel(_ state: TaskState) -> String {
        state == .done && autoDone ? "自动完成" : state.todoLabel
    }

    /// Why the hub closed it, when it did.
    var autoDoneReason: String? {
        guard autoDone, let r = doneReason?.trimmingCharacters(in: .whitespacesAndNewlines), !r.isEmpty else { return nil }
        return r
    }
}

nonisolated extension TaskState {
    var todoLabel: String {
        switch self {
        case .done: "已完成"
        case .needsYou: "等你"
        case .unread: "待查看"
        case .working: "进行中"
        case .followup: "待跟进"
        case .notStarted: "未开始"
        case .failed: "没开起来"
        }
    }

    var todoRank: Int {
        switch self {
        case .needsYou: 0
        case .unread: 1
        case .working: 2
        case .followup: 3
        case .notStarted: 4
        case .done: 5
        case .failed: 6
        }
    }

    var todoTone: StatusTone {
        switch self {
        case .done: StatusPalette.idle
        case .needsYou: StatusPalette.needsYou
        case .unread: StatusPalette.unread
        case .working: StatusPalette.working
        case .followup: StatusPalette.idle
        case .notStarted: StatusPalette.idle
        case .failed: StatusPalette.idle
        }
    }
}

nonisolated struct TodoMostUrgent: Sendable {
    var agent: TodoItem.Agent
    var session: Session?
    var status: AgentState
    var isFailed: Bool
}

nonisolated struct TodoAgentGroup: Identifiable, Sendable {
    var key: String
    var name: String
    var sessions: [Session]
    var todoID: String?
    var todoStatus: TaskState?
    /// Starts folded (未归属 below todos with nothing in it waiting for you, #1836).
    var folded = false

    var id: String { key }
}

nonisolated enum TodosLogic {
    /// A successfully loaded empty list allows creating the first todo; nonempty lists need an explicit array.
    /// The store keeps support unknown until its first successful response.
    static func supportsSpaces(_ todos: [TodoItem]) -> Bool {
        todos.isEmpty || todos.contains { $0.spaces != nil }
    }

    /// Modern hub JSON errors always carry a code; older hubs without this route return plain-text 404s.
    /// Reads the raw fields (the status, `code` and the router's literal "Not Found"), not `message`, which is text for people.
    static func isLegacySpacesRoute(_ error: Error) -> Bool {
        guard let api = error as? APIError, api.status == 404 else { return false }
        let code = api.fields["code"] ?? ""
        return code.isEmpty || (code == "not_found" && api.fields["error"] == "Not Found")
    }

    static func findTodoForSpace(_ todos: [TodoItem], machineID: String, space: String) -> TodoItem? {
        guard !machineID.isEmpty, !space.isEmpty else { return nil }
        return todos.first { todo in
            !todo.isDone && (todo.spaces ?? []).contains { $0.machineID == machineID && $0.space == space }
        }
    }

    /// Each path component follows encodeURIComponent, including slashes inside a project name.
    static func spacePath(todoID: String, machineID: String, space: String) -> String {
        let allowed = CharacterSet(charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()")
        func encoded(_ value: String) -> String { value.addingPercentEncoding(withAllowedCharacters: allowed) ?? value }
        return "/api/v1/todos/\(encoded(todoID))/spaces/\(encoded(machineID))/\(encoded(space))"
    }

    static func agentSource(_ agent: TodoItem.Agent, lookup: SessionLookup? = nil) -> String? {
        switch agent.source {
        case "space":
            let via = agent.via ?? ""
            let name = via.firstIndex(of: "/").map { String(via[via.index(after: $0)...]) } ?? via
            return name.isEmpty ? "随项目" : "随项目 \(name)"
        case "spawned":
            if let via = agent.via, let slash = via.firstIndex(of: "/"),
               let parent = lookup?(String(via[..<slash]), String(via[via.index(after: slash)...])),
               !parent.title.isEmpty {
                return "由 \(parent.title) 派出"
            }
            return "派生"
        case "chat": return "同群"
        default: return nil
        }
    }

    /// Whether a project of a todo has an Agent running on it (else the detail says 「当前没有 Agent」): a live session
    /// in that project, on the todos' hub group (`isOnTodosHub`: another group's computer may share the id and the
    /// project's name), on a computer that is connected (as the web: a disconnected computer's sessions turn unknown,
    /// not offline).
    static func hasLiveAgent(in space: TodoItem.Space, sessions: [Session], hubGroup: String?,
                             machineOnline: (String) -> Bool) -> Bool {
        guard machineOnline(space.machineID) else { return false }
        return sessions.contains { session in
            guard session.machineID == space.machineID, session.state.isLive,
                  isOnTodosHub(sessionGroup: session.group, hubGroup: hubGroup) else { return false }
            let name = session.spaceName.flatMap { $0.isEmpty ? nil : $0 } ?? session.spaceID
            return name == space.space
        }
    }

    static func createdLinkFailure(_ error: Error, isSpace: Bool) -> String {
        "待办已创建，但\(isSpace ? "挂到待办" : "关联")失败：\(error.localizedDescription)"
    }

    /// Whether a session may take a todo's actions here. The todos are the selected hub's (`AppModel.hub`), so a
    /// session on another hub group would send its request to the wrong hub and find no todo.
    static func isOnTodosHub(sessionGroup: String, hubGroup: String?) -> Bool {
        sessionGroup == (hubGroup ?? "")
    }

    /// The Agents of the given todos, read one by one through `session` (a view reads one session's own cell, not the
    /// whole list). Only Agents on `hubGroup` can be linked (`isOnTodosHub`), so the keys are built on that group.
    static func linkedSessions(of todos: [TodoItem], hubGroup: String, session: (SessionKey) -> Session?) -> [Session] {
        todos.flatMap { todo in
            todo.agents.compactMap { a in
                session(SessionKey(group: hubGroup, machineID: a.machineID, sessionID: a.sessionID))
            }
        }
    }

    /// GET /api/v1/todos answered 404: an older hub without todos. The entrance is hidden and nothing is shown as an error.
    public static func isOldHub(_ error: Error) -> Bool {
        (error as? APIError)?.status == 404
    }

    public typealias SessionLookup = @Sendable (_ machineID: String, _ sessionID: String) -> Session?

    public static func makeSessionLookup(_ sessions: [Session]) -> SessionLookup {
        var map = [String: Session]()
        for s in sessions {
            map["\(s.machineID)/\(s.sessionID)"] = s
        }
        let immutableMap = map
        return { mid, sid in immutableMap["\(mid)/\(sid)"] }
    }

    /**
     * TodoItem status, first match wins:
     * 1. Done (已完成): the user marked it done (done_at != nil)
     * 2. Needs you (等你): an attached agent needs the user (a question or an approval)
     * 3. Unread (待查看): an attached agent finished and the user has not looked yet
     * 4. Working (进行中): an attached agent is working or starting
     * 5. Follow-up (待跟进): every attached agent is idle (seen, not running, or failed to start)
     * 6. Not started (未开始): no attached agent
     */
    public static func todoStatus(_ todo: TodoItem, lookup: SessionLookup? = nil) -> TaskState {
        let states: [AgentState] = todo.agents.filter { $0.releasedAt == nil }.map { agent in
            if let error = agent.startError, !error.isEmpty { return .idle }
            guard let session = lookup?(agent.machineID, agent.sessionID) else { return .offline }
            if session.state == .needsYou || session.state == .unread { return session.state }
            return session.starting == true ? .working : session.state
        }
        return TaskState(todo: todo, agents: states)
    }

    /**
     * Sorts the todo list:
     * - open todos first, by status priority (needs you > unread > working > follow-up > not started)
     * - same status: newer first, by creation time
     * - done todos at the bottom
     */
    public static func sortTodos(_ todos: [TodoItem], lookup: SessionLookup? = nil) -> [TodoItem] {
        todos.sorted { a, b in
            let aDone = a.isDone
            let bDone = b.isDone
            if aDone != bDone { return !aDone }

            let aStat = todoStatus(a, lookup: lookup)
            let bStat = todoStatus(b, lookup: lookup)
            if aStat.todoRank != bStat.todoRank { return aStat.todoRank < bStat.todoRank }

            let tA = a.createdAt?.timeIntervalSince1970 ?? 0
            let tB = b.createdAt?.timeIntervalSince1970 ?? 0
            if tA != tB { return tA > tB }
            return a.id < b.id
        }
    }

    /**
     * Finds the most urgent attached agent of a todo, with its summary
     */
    public static func mostUrgentAgent(_ todo: TodoItem, lookup: SessionLookup? = nil) -> TodoMostUrgent? {
        let active = todo.agents.filter { $0.releasedAt == nil }
        if active.isEmpty { return nil }

        func score(_ st: AgentState, isFailed: Bool) -> Int {
            if isFailed { return 1 }
            switch st {
            case .needsYou: return 4
            case .unread: return 3
            case .working: return 2
            default: return 0
            }
        }

        var best: TodoMostUrgent?
        var bestScore = -1

        for a in active {
            let isFailed = a.startError.map { !$0.isEmpty } ?? false
            let s = lookup?(a.machineID, a.sessionID)
            let st = isFailed ? .offline : (s?.state ?? .offline)
            let sc = score(st, isFailed: isFailed)
            if sc > bestScore || best == nil {
                bestScore = sc
                best = TodoMostUrgent(agent: a, session: s, status: st, isFailed: isFailed)
            }
        }

        return best
    }

    private static func sessionRank(_ s: Session, isMuted: (Session) -> Bool) -> Int {
        let state = s.state
        let waiting = (state == .idle || state == .unknown) && (s.loop != nil || !(s.background ?? []).isEmpty || (s.subagents ?? []).contains(where: \.running))
        let muted = isMuted(s)
        if state == .needsYou { return 0 }
        if muted && (state == .working || state == .unread || waiting) { return 4 }
        if state == .unread { return 1 }
        if state == .working { return 2 }
        if waiting { return 3 }
        return 5
    }

    /// An agent opened by hand in herdr: not started by Persona (nor spawned through it).
    static func isManualHerdr(_ s: Session) -> Bool {
        s.spaceKind == "herdr" && !s.startedByJarvis
    }

    private static func byDate(_ a: Session, _ b: Session) -> Bool {
        if a.sortDate != b.sortDate { return a.sortDate > b.sortDate }
        if a.sessionID != b.sessionID { return a.sessionID < b.sessionID }
        return a.machineID < b.machineID
    }

    /**
     * Pure grouping for the Agents sidebar, by todo:
     * - one section per open todo with attached agents; the header shows the todo title and status, and tapping it opens the todo
     * - agents with no todo opened by hand in herdr get their own section, "手动开的", open, before 未归属 (#2608, #2917)
     * - the other agents with no todo go into the last section, "未归属": folded below todos unless one of them needs you
     *   (unread alone stays folded, many lists always have some); with no todo above it is the whole list, open (#1836)
     * - pinning, muting and the "最近（未运行）" rules match the "按状态" grouping
     */
    public static func buildTodoAgentGroups(
        sessions: [Session],
        todos: [TodoItem],
        isMuted: (Session) -> Bool
    ) -> [TodoAgentGroup] {
        let live = sessions.filter { $0.state.isLive }
        let lookup = makeSessionLookup(live)

        let openTodos = sortTodos(todos.filter { !$0.isDone }, lookup: lookup)

        var liveByKey = [String: Session]()
        for s in live {
            liveByKey["\(s.machineID)/\(s.sessionID)"] = s
        }

        var assignedSessionKeys = Set<String>()
        var groups: [TodoAgentGroup] = []

        for todo in openTodos {
            var todoSessions: [Session] = []
            let activeAgents = todo.agents.filter { $0.releasedAt == nil }

            for a in activeAgents {
                let key = "\(a.machineID)/\(a.sessionID)"
                if let s = liveByKey[key], !assignedSessionKeys.contains(key) {
                    todoSessions.append(s)
                    assignedSessionKeys.insert(key)
                }
            }

            for s in live {
                let key = "\(s.machineID)/\(s.sessionID)"
                if s.todo?.id == todo.id, !assignedSessionKeys.contains(key) {
                    todoSessions.append(s)
                    assignedSessionKeys.insert(key)
                }
            }

            if !todoSessions.isEmpty {
                todoSessions.sort { a, b in
                    let rA = sessionRank(a, isMuted: isMuted)
                    let rB = sessionRank(b, isMuted: isMuted)
                    if rA != rB { return rA < rB }
                    return byDate(a, b)
                }

                let st = todoStatus(todo, lookup: lookup)
                groups.append(TodoAgentGroup(
                    key: "todo:\(todo.id)",
                    name: todo.title,
                    sessions: todoSessions,
                    todoID: todo.id,
                    todoStatus: st
                ))
            }
        }

        let byRank = { (a: Session, b: Session) -> Bool in
            let rA = sessionRank(a, isMuted: isMuted)
            let rB = sessionRank(b, isMuted: isMuted)
            if rA != rB { return rA < rB }
            return byDate(a, b)
        }
        let rest = live.filter { !assignedSessionKeys.contains("\($0.machineID)/\($0.sessionID)") }

        // Opened by hand in herdr and on no todo: no auto-link rule knows them (not spawned, their space is on no
        // todo), so they get a section of their own, open, before 未归属 (#2608, #2917).
        let manual = rest.filter(isManualHerdr).sorted(by: byRank)
        if !manual.isEmpty {
            groups.append(TodoAgentGroup(key: "todo:manual", name: "手动开的", sessions: manual, todoID: nil, todoStatus: nil))
        }

        let unassigned = rest.filter { !isManualHerdr($0) }
        if !unassigned.isEmpty {
            let sortedUnassigned = unassigned.sorted(by: byRank)
            groups.append(TodoAgentGroup(
                key: "todo:none",
                name: "未归属",
                sessions: sortedUnassigned,
                todoID: nil,
                todoStatus: nil,
                folded: !groups.isEmpty && !sortedUnassigned.contains { $0.state == .needsYou }
            ))
        }

        return groups
    }

    /**
     * Format of the context attached when a todo is handed to an Agent (the same as the inbox's)
     * `<jarvis-context kind="context" title="待办 · <标题>" url="">说明</jarvis-context>`
     */
    public static func formatTodoContext(title: String, notes: String?) -> String {
        let t = title
            .replacingOccurrences(of: "&", with: "&amp;")
            .replacingOccurrences(of: "\"", with: "&quot;")
            .replacingOccurrences(of: "<", with: "&lt;")
            .replacingOccurrences(of: ">", with: "&gt;")
        let body = (notes ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        return "<jarvis-context kind=\"context\" title=\"待办 · \(t)\" url=\"\">\(body)</jarvis-context>"
    }
}

// MARK: - APIClient extensions for Todos

extension APIClient {
    struct TodoResponse: Decodable, Sendable {
        var todo: TodoItem
    }

    func linkTodoSpace(todoID: String, machineID: String, space: String) async throws {
        _ = try await request("PUT", TodosLogic.spacePath(todoID: todoID, machineID: machineID, space: space))
    }

    func unlinkTodoSpace(todoID: String, machineID: String, space: String) async throws {
        _ = try await request("DELETE", TodosLogic.spacePath(todoID: todoID, machineID: machineID, space: space))
    }

    public struct CreateTodoRequest: Encodable, Sendable {
        var title: String
        var notes: String?
    }

    public func createTodo(title: String, notes: String? = nil) async throws -> TodoItem {
        let body = CreateTodoRequest(title: title.trimmingCharacters(in: .whitespacesAndNewlines), notes: notes?.trimmingCharacters(in: .whitespacesAndNewlines))
        return try await post("/api/v1/todos", body: body, as: TodoResponse.self).todo
    }

    public struct UpdateTodoRequest: Encodable, Sendable {
        var title: String?
        var notes: String?
        var done: Bool?
    }

    public func updateTodo(id: String, title: String? = nil, notes: String? = nil, done: Bool? = nil) async throws -> TodoItem {
        let encId = id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? id
        let body = UpdateTodoRequest(title: title, notes: notes, done: done)
        let data = try await request("PATCH", "/api/v1/todos/\(encId)", body: body)
        return try JSON.decoder.decode(TodoResponse.self, from: data).todo
    }

    public func deleteTodo(id: String) async throws {
        let encId = id.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? id
        _ = try await request("DELETE", "/api/v1/todos/\(encId)")
    }

    public func linkTodoAgent(todoID: String, machineID: String, sessionID: String) async throws {
        let encTodo = todoID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? todoID
        let encMid = machineID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? machineID
        let encSid = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        _ = try await request("PUT", "/api/v1/todos/\(encTodo)/agents/\(encMid)/\(encSid)")
    }

    public func unlinkTodoAgent(todoID: String, machineID: String, sessionID: String) async throws {
        let encTodo = todoID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? todoID
        let encMid = machineID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? machineID
        let encSid = sessionID.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) ?? sessionID
        _ = try await request("DELETE", "/api/v1/todos/\(encTodo)/agents/\(encMid)/\(encSid)")
    }
}

/// The text a todo's detail page edits: its title and notes. The page takes the stored text when it shows a todo, and
/// keeps what the user is typing, so one todo's text never lands in another todo's fields.
nonisolated struct TodoEditorText: Equatable {
    var title: String
    var notes: String

    init(title: String = "", notes: String = "") {
        self.title = title
        self.notes = notes
    }

    init(_ todo: TodoItem) {
        title = todo.title
        notes = todo.notes ?? ""
    }

    /// Takes the stored text of `todo`, except in a field the user is editing.
    mutating func reseed(from todo: TodoItem, editingTitle: Bool, editingNotes: Bool) {
        if !editingTitle { title = todo.title }
        if !editingNotes { notes = todo.notes ?? "" }
    }
}

/// The session marker remains authoritative even before the todo list is available.
nonisolated enum TodoLinkTarget: Equatable, Sendable {
    case agent(machineID: String, sessionID: String, currentTodoID: String?)
    case space(TodoItem.Space)

    var id: String {
        switch self {
        case .agent(let machine, let session, _): "agent/\(machine)/\(session)"
        case .space(let project): "space/\(project.machineID)/\(project.space)"
        }
    }

    var isSpace: Bool { if case .space = self { true } else { false } }

    func currentTodoID(in todos: [TodoItem]) -> String? {
        switch self {
        case .agent(_, _, let current): current
        case .space(let project): TodosLogic.findTodoForSpace(todos, machineID: project.machineID, space: project.space)?.id
        }
    }
}

nonisolated struct TodoLinkDraft: Equatable {
    var selectedTodoID = ""
    var creating = false
    var newTitle = ""
    var busy = false

    mutating func reset(currentTodoID: String?) {
        self = TodoLinkDraft(selectedTodoID: currentTodoID ?? "")
    }
}
