import Foundation
import Observation

/// Tasks from the hub-level group's assistant conversations (docs/unified-entry.md): the todos that are tasks, the
/// plans and each conversation's feed. Pulled when a conversation shows and on (re)connect, kept up to date by the
/// hub's `todos`, `assistant_feed` and `task_plan` events. What a task's agents are doing comes from `AppModel`'s
/// sessions and interactions, so a card follows them like the Agents page does.
@MainActor @Observable
final class TasksStore {
    static let shared = TasksStore()

    private(set) var todos: [String: TodoItem] = [:]
    private(set) var plans: [String: TaskPlan] = [:]
    /// Conversation id → its feed, oldest first.
    private(set) var feeds: [String: [FeedItem]] = [:]
    /// Conversation id → the task the next message goes straight to (「对「xx」说」), set by @ or 说一句.
    var routes: [String: String] = [:]
    /// The task whose detail sheet is open, and whether the board is.
    var detailTask: String?
    var showBoard = false
    /// A task picked on the board, opened when the board has gone down.
    var openAfterBoard: String?
    /// Plans being started or dismissed (their card shows it and takes no second tap).
    private(set) var busyPlans: Set<String> = []
    var error: String?

    @ObservationIgnored private var reloadingTodos: Task<Void, Never>?
    @ObservationIgnored private var loading: [String: Task<Void, Never>] = [:]

    private init() {}

    /// Entering or leaving 体验模式, or another hub: nothing of the other side may show.
    func forget() {
        todos = [:]
        plans = [:]
        feeds = [:]
        routes = [:]
        detailTask = nil
        showBoard = false
        busyPlans = []
    }

    // MARK: Loading

    func reloadTodos() {
        guard reloadingTodos == nil, let api = AppModel.shared.api else { return }
        reloadingTodos = Task {
            defer { reloadingTodos = nil }
            if let list = try? await api.todos() {
                var map: [String: TodoItem] = [:]
                for t in list { map[t.id] = t }
                if map != todos { todos = map }
            }
        }
    }

    /// Pulls a conversation's feed (what came since the newest item it has) and the todos.
    func load(_ conversation: String) {
        guard loading[conversation] == nil, let api = AppModel.shared.api else { return }
        reloadTodos()
        let since = feeds[conversation]?.last?.at
        loading[conversation] = Task {
            defer { loading[conversation] = nil }
            guard let res = try? await api.feed(conversation: conversation, since: since) else { return }
            for (id, plan) in res.plans ?? [:] where plans[id] != plan { plans[id] = plan }
            merge(res.items, into: conversation)
        }
    }

    /// The (re)connect: every conversation shown so far, again.
    func reconnected() {
        reloadTodos()
        for c in feeds.keys { load(c) }
    }

    func received(_ item: FeedItem) {
        merge([item], into: item.conversation)
        if item.kind == .task || item.kind == .receipt { reloadTodos() }
    }

    func received(_ plan: TaskPlan) {
        if plans[plan.id] != plan { plans[plan.id] = plan }
    }

    private func merge(_ items: [FeedItem], into conversation: String) {
        guard !items.isEmpty else {
            if feeds[conversation] == nil { feeds[conversation] = [] }
            return
        }
        var list = feeds[conversation] ?? []
        let have = Set(list.map(\.id))
        let new = items.filter { !have.contains($0.id) }
        guard !new.isEmpty else { return }
        list += new
        list.sort { ($0.at, $0.id) < ($1.at, $1.id) }
        feeds[conversation] = list
    }

    // MARK: What a conversation shows

    func feed(_ conversation: String) -> [FeedItem] { feeds[conversation] ?? [] }

    /// The tasks the conversation opened, newest first.
    func tasks(of conversation: String) -> [TodoItem] {
        todos.values.filter { $0.origin == conversation }
            .sorted { ($0.createdAt ?? .distantPast) > ($1.createdAt ?? .distantPast) }
    }

    /// The task's agents as the app knows them now (the Agents page's sessions).
    func sessions(_ todo: TodoItem) -> [Session] {
        let model = AppModel.shared
        let group = model.hub?.group ?? ""
        return todo.activeAgents.compactMap { model.session(SessionKey(group: group, machineID: $0.machineID, sessionID: $0.sessionID)) }
    }

    func state(_ todo: TodoItem) -> TaskState {
        TaskState(todo: todo, agents: sessions(todo).map(\.state))
    }

    /// The task's agent to open first: the one that waits for the user, then the one working, then the latest.
    func leadSession(_ todo: TodoItem) -> Session? {
        sessions(todo).min { a, b in
            a.state.priority != b.state.priority ? a.state.priority < b.state.priority : (a.updatedAt ?? .distantPast) > (b.updatedAt ?? .distantPast)
        }
    }

    /// What the task's agents are asking the user, oldest first.
    func interactions(_ todo: TodoItem) -> [Interaction] {
        let keys = Set(sessions(todo).map(\.key))
        return AppModel.shared.pendingInteractions.filter { keys.contains($0.sessionKey) }
    }

    /// The dock: the conversation's tasks still going on, or finished and not looked at; what waits first.
    func dock(_ conversation: String) -> [TodoItem] {
        let rows: [(todo: TodoItem, state: TaskState)] = tasks(of: conversation).map { ($0, state($0)) }
        let going = rows.filter { $0.state != .done && $0.state != .followup }
        let sorted = going.sorted { a, b in
            if a.state.priority != b.state.priority { return a.state.priority < b.state.priority }
            return (a.todo.createdAt ?? .distantPast) > (b.todo.createdAt ?? .distantPast)
        }
        return sorted.map(\.todo)
    }

    // MARK: Actions

    func start(_ plan: TaskPlan, edits: [PlanTask]? = nil, remember: Bool = false) async {
        guard let api = AppModel.shared.api, !busyPlans.contains(plan.id) else { return }
        busyPlans.insert(plan.id)
        defer { busyPlans.remove(plan.id) }
        do {
            received(try await api.startPlan(plan.id, edits: edits, remember: remember))
            reloadTodos()
            Haptics.success()
        } catch {
            self.error = error.localizedDescription
            AppModel.shared.toast = Toast("没开起来：\(error.localizedDescription)", style: .error)
        }
    }

    func dismiss(_ plan: TaskPlan) async {
        guard let api = AppModel.shared.api, !busyPlans.contains(plan.id) else { return }
        busyPlans.insert(plan.id)
        defer { busyPlans.remove(plan.id) }
        do {
            received(try await api.dismissPlan(plan.id))
        } catch {
            AppModel.shared.toast = Toast(error.localizedDescription, style: .error)
        }
    }

    /// Sends text straight to the task the conversation is routed to; false when there was none (send it as usual).
    func sendRouted(_ conversation: String, text: String) -> Bool {
        guard let todoID = routes[conversation], let api = AppModel.shared.api else { return false }
        routes[conversation] = nil
        Task {
            do {
                received(try await api.route(conversation: conversation, todoID: todoID, text: text))
            } catch {
                AppModel.shared.toast = Toast("没送到：\(error.localizedDescription)", style: .error)
            }
        }
        return true
    }
}
