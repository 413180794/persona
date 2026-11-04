import Foundation
import Observation

/// Task presentation for the hub-level group's assistant conversations (docs/unified-entry.md): todos come from
/// TodoStore's shared index; plans and each conversation's feed stay here. Feeds refresh when shown and on
/// reconnect, with assistant_feed and task_plan events keeping them current. A task's agents come from AppModel's
/// sessions and interactions, so a card follows them like the Agents page does.
@MainActor @Observable
final class TasksStore {
    static let shared = TasksStore()

    var todos: [String: TodoItem] { todoStore.byID }
    private(set) var plans: [String: TaskPlan] = [:]
    /// Conversation id → its feed, oldest first.
    private(set) var feeds: [String: [FeedItem]] = [:]
    /// Conversation id → the task the next message goes straight to (「对「xx」说」), set by @ or 说一句.
    var routes: [String: String] = [:]
    /// The task whose detail sheet is open, and whether the board is.
    var detailTask: String?
    /// The agent (not a task's, #1113) whose page the detail sheet shows instead.
    var detailAgent: SessionKey?
    var showBoard = false
    /// A task or an agent picked on the board, opened when the board has gone down.
    var openAfterBoard: String?
    var openAgentAfterBoard: SessionKey?
    /// Message cards (inbox item ids) put off with 稍后, and the ones sent from their card.
    private(set) var laterMessages: Set<String> = []
    private(set) var sentMessages: Set<String> = []
    /// Quick results (QuickResult.callID) the user took back with 撤销.
    private(set) var undone: Set<String> = []
    /// Plans being started or dismissed (their card shows it and takes no second tap).
    private(set) var busyPlans: Set<String> = []
    var error: String?

    @ObservationIgnored private let todoStore: TodoStore
    @ObservationIgnored private var loading: [String: Task<Void, Never>] = [:]
    /// Conversation id → the day its 今天 card was asked for (once a day).
    @ObservationIgnored private var todayAsked: [String: String] = [:]

    init(todoStore: TodoStore = .shared) { self.todoStore = todoStore }

    /// Entering or leaving 体验模式, or another hub: nothing of the other side may show.
    func forget() {
        plans = [:]
        feeds = [:]
        routes = [:]
        detailTask = nil
        detailAgent = nil
        showBoard = false
        busyPlans = []
    }

    // MARK: Loading

    func reloadTodos(api: APIClient? = nil) {
        todoStore.reload(api: api)
    }

    /// Pulls a conversation's feed (what came since the newest item it has) and the todos.
    @discardableResult
    func load(_ conversation: String, refreshTodos: Bool = true, api injected: APIClient? = nil) -> Task<Void, Never>? {
        if let task = loading[conversation] { return task }
        guard let api = injected ?? AppModel.shared.api else { return nil }
        if refreshTodos { reloadTodos(api: injected) }
        let since = feeds[conversation]?.last?.at
        let task = Task {
            defer { loading[conversation] = nil }
            guard let res = try? await api.feed(conversation: conversation, since: since) else { return }
            for (id, plan) in res.plans ?? [:] where plans[id] != plan { plans[id] = plan }
            merge(res.items, into: conversation)
        }
        loading[conversation] = task
        return task
    }

    /// The (re)connect: every conversation shown so far, again.
    @discardableResult
    func reconnected(api: APIClient? = nil) -> [Task<Void, Never>] {
        feeds.keys.compactMap { load($0, refreshTodos: false, api: api) }
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

    /// The agents going on that the conversation's dock and board show after its tasks (#1113): in the main
    /// conversation the ones no task stands for (opened by hand on the Agents page); in the others none.
    func agents(of conversation: String) -> [DockAgent] {
        let model = AppModel.shared
        guard let main = model.assistantMain?.sessionID, conversation == main else { return [] }
        let group = model.hub?.group ?? ""
        let tasks = todos.values.filter { !($0.origin ?? "").isEmpty && $0.doneAt == nil }
        let keys = Set(tasks.flatMap { t in
            t.activeAgents.map { SessionKey(group: group, machineID: $0.machineID, sessionID: $0.sessionID) }
        })
        let manual = todos.values.filter { ($0.origin ?? "").isEmpty }.map(\.id)
        return DockAgent.shown(model.goingAgents, conversation: conversation, main: main, group: group,
                               taskAgents: keys, manualTodos: Set(manual))
    }

    /// The task (a todo from an assistant conversation, not done) an agent works for.
    func task(of key: SessionKey) -> TodoItem? {
        guard key.group == (AppModel.shared.hub?.group ?? "") else { return nil }
        return todos.values.first { t in
            t.origin != nil && t.doneAt == nil && t.activeAgents.contains { $0.machineID == key.machineID && $0.sessionID == key.sessionID }
        }
    }

    /// The task's agents as the app knows them now (the Agents page's sessions), with their last line and step: a card
    /// follows each new step of an agent that only runs tools (`AppModel.sessionWithLine(_:)`).
    func sessions(_ todo: TodoItem) -> [Session] {
        let model = AppModel.shared
        let group = model.hub?.group ?? ""
        return todo.activeAgents.compactMap { model.sessionWithLine(SessionKey(group: group, machineID: $0.machineID, sessionID: $0.sessionID)) }
    }

    func state(_ todo: TodoItem) -> TaskState {
        let state = TaskState(todo: todo, agents: sessions(todo).map(\.state))
        // A hub from before #1077 leaves the todo open when the agent did not start; its plan still says why.
        if state == .notStarted, todo.agents.isEmpty, planError(todo) != nil { return .failed }
        return state
    }

    /// Why the task's agent did not start: the todo's own word, else its plan's (nil while it may still start).
    func startFailure(_ todo: TodoItem) -> String? {
        if let e = todo.startError, !e.isEmpty { return e }
        return todo.agents.isEmpty ? planError(todo) : nil
    }

    private func planError(_ todo: TodoItem) -> String? {
        guard let plan = todo.planID.flatMap({ plans[$0] }),
              let error = plan.tasks.first(where: { $0.todoID == todo.id })?.error, !error.isEmpty else { return nil }
        return error
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
        let going = rows.filter { $0.state != .done && $0.state != .followup && $0.state != .failed }
        let sorted = going.sorted { a, b in
            if a.state.priority != b.state.priority { return a.state.priority < b.state.priority }
            return (a.todo.createdAt ?? .distantPast) > (b.todo.createdAt ?? .distantPast)
        }
        return sorted.map(\.todo)
    }

    /// The main conversation's 今天 card, once a day: the hub makes it on the first ask and answers it after that.
    func loadToday(_ conversation: String) {
        let day = Date.now.formatted(.iso8601.year().month().day())
        guard todayAsked[conversation] != day, let api = AppModel.shared.api else { return }
        todayAsked[conversation] = day
        Task {
            if let item = try? await api.today(conversation: conversation) { received(item) } else { todayAsked[conversation] = nil }
        }
    }

    // MARK: Actions

    /// 发送 on a message card: the draft (as edited) goes out through the inbox, which files the item as done.
    func sendMessage(_ itemID: String, text: String) async {
        let inbox = InboxStore.shared
        if let item = inbox.items.first(where: { $0.id == itemID }) {
            if await inbox.send(item, text: text) { sentMessages.insert(itemID) }
            return
        }
        guard let api = AppModel.shared.api else { return }
        do {
            try await api.inboxReply(itemID, text: text)
            sentMessages.insert(itemID)
            Haptics.success()
            inbox.reload()
        } catch {
            AppModel.shared.toast = Toast("回复没发出去：\(error.localizedDescription)", style: .error)
        }
    }

    func later(_ itemID: String) { laterMessages.insert(itemID) }

    /// Whether the card's message was answered: sent from the card, or replied / filed in the inbox.
    func messageSettled(_ itemID: String) -> Bool {
        sentMessages.contains(itemID) || InboxStore.shared.items.first { $0.id == itemID }?.isDone == true
    }

    /// 撤销 on a quick result: the reminder or event comes off the phone.
    func undo(_ result: QuickResult) async {
        do {
            switch result.kind {
            case .reminder: _ = try RemindersTool.remove(id: result.itemID)
            case .event: _ = try CalendarTool.remove(id: result.itemID)
            }
            undone.insert(result.callID)
            Haptics.success()
        } catch {
            AppModel.shared.toast = Toast("没撤销成功：\(error.localizedDescription)", style: .error)
        }
    }

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

    /// 移除 on a task that does not run: the todo goes (its agents and their chats stay on the Agents page), and with
    /// it the task's card, pill and line.
    func remove(_ todo: TodoItem) async {
        guard let api = AppModel.shared.api else { return }
        do {
            try await todoStore.delete(id: todo.id, api: api)
            for (conversation, id) in routes where id == todo.id { routes[conversation] = nil }
            if detailTask == todo.id { detailTask = nil }
            Haptics.success()
        } catch {
            AppModel.shared.toast = Toast("没移除：\(error.localizedDescription)", style: .error)
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
