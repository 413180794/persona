import Foundation
import Observation

/// Hub-wide todo list management (docs/todos.md, issue #401):
/// The hub's todos, cached locally in StateCache, refreshed on {"type":"todos"} WS event and (re)connect.
/// Supports 404 gracefully (older hubs without todos support: hides entrance, no error).
@MainActor @Observable
final class TodoStore {
    static let shared = TodoStore()

    private(set) var todos: [TodoItem] = []
    private(set) var loading = false
    /// false when GET /api/v1/todos returned 404 (old hub without todos)
    private(set) var isSupported = true
    private(set) var spacesSupported = false
    private(set) var changingSpaces = false
    var error: String?

    @ObservationIgnored private var reloading: Task<Void, Never>?
    @ObservationIgnored private let saver = Debouncer(.seconds(1))
    private static let stateName = "todo-items"

    private init() {
        todos = StateCache.load(Self.stateName, as: [TodoItem].self) ?? []
    }

    var openTodos: [TodoItem] { todos.filter { !$0.isDone } }
    var openCount: Int { openTodos.count }

    func forget() {
        todos = []
        error = nil
        isSupported = true
        spacesSupported = false
        saver.run { StateCache.save(Self.stateName, [TodoItem]()) }
    }

    func reload() {
        guard reloading == nil else { return }
        reloading = Task {
            defer { reloading = nil }
            await fetch()
        }
    }

    /// `api` is the client to read through (tests pass a fake hub's); the app reads the current hub connection.
    func refresh(api: APIClient? = nil) async {
        await fetch(api: api)
    }

    private func fetch(api injected: APIClient? = nil) async {
        guard let api = injected ?? AppModel.shared.api else { return }
        loading = true
        defer { loading = false }
        do {
            let list = try await api.todos()
            isSupported = true
            spacesSupported = TodosLogic.supportsSpaces(list)
            apply(list)
            error = nil
        } catch let apiErr where TodosLogic.isOldHub(apiErr) {
            // Older hub without todos: hide entrance without error
            isSupported = false
            spacesSupported = false
            error = nil
        } catch {
            if let shown = error.shown { self.error = shown }
        }
    }

    private func apply(_ list: [TodoItem]) {
        guard list != todos else { return }
        todos = list
        let snapshot = list
        saver.run { StateCache.save(Self.stateName, snapshot) }
    }

    func todo(id: String) -> TodoItem? {
        todos.first { $0.id == id }
    }

    func create(title: String, notes: String? = nil, api injected: APIClient? = nil) async throws -> TodoItem {
        guard let api = injected ?? AppModel.shared.api else { throw APIError(status: 0, message: "未连接 hub") }
        let created = try await api.createTodo(title: title, notes: notes)
        var updated = todos
        updated.insert(created, at: 0)
        apply(updated)
        return created
    }

    func update(id: String, title: String? = nil, notes: String? = nil, done: Bool? = nil, api injected: APIClient? = nil) async throws -> TodoItem {
        guard let api = injected ?? AppModel.shared.api else { throw APIError(status: 0, message: "未连接 hub") }
        let res = try await api.updateTodo(id: id, title: title, notes: notes, done: done)
        var updated = todos
        if let idx = updated.firstIndex(where: { $0.id == id }) {
            updated[idx] = res
        } else {
            updated.insert(res, at: 0)
        }
        apply(updated)
        return res
    }

    /// `api` is the client to write through (tests pass a fake hub's); the app uses the current hub connection.
    func toggleDone(_ todo: TodoItem, api: APIClient? = nil) {
        let newDone = !todo.isDone
        let original = todos
        // Optimistic update
        var updated = todos
        if let idx = updated.firstIndex(where: { $0.id == todo.id }) {
            updated[idx].doneAt = newDone ? Date() : nil
            apply(updated)
        }
        Task {
            do {
                _ = try await update(id: todo.id, done: newDone, api: api)
            } catch {
                // Revert on failure: `todos` already holds the optimistic list, so put back the one saved before it.
                apply(original)
                AppModel.shared.show(error)
            }
        }
    }

    func delete(id: String) async throws {
        guard let api = AppModel.shared.api else { throw APIError(status: 0, message: "未连接 hub") }
        try await api.deleteTodo(id: id)
        var updated = todos
        updated.removeAll { $0.id == id }
        apply(updated)
    }

    func linkAgent(todoID: String, machineID: String, sessionID: String, api injected: APIClient? = nil) async throws {
        guard let api = injected ?? AppModel.shared.api else { throw APIError(status: 0, message: "未连接 hub") }
        do {
            try await api.linkTodoAgent(todoID: todoID, machineID: machineID, sessionID: sessionID)
        } catch {
            await fetch(api: injected)
            throw error
        }
        await fetch(api: injected)
    }

    func unlinkAgent(todoID: String, machineID: String, sessionID: String, api injected: APIClient? = nil) async throws {
        guard let api = injected ?? AppModel.shared.api else { throw APIError(status: 0, message: "未连接 hub") }
        do {
            try await api.unlinkTodoAgent(todoID: todoID, machineID: machineID, sessionID: sessionID)
        } catch {
            await fetch(api: injected)
            throw error
        }
        await fetch(api: injected)
    }

    /// Membership changes immediately; the hub then supplies the authoritative Agent attachments.
    @discardableResult
    func setSpace(todoID: String, space: TodoItem.Space, linked: Bool, api injected: APIClient? = nil) async throws -> Bool {
        guard !changingSpaces else { throw APIError(status: 409, message: "正在更新项目，请稍后再试") }
        guard let api = injected ?? AppModel.shared.api else { throw APIError(status: 0, message: "未连接 hub") }
        changingSpaces = true
        defer { changingSpaces = false }
        let original = todos
        let via = "\(space.machineID)/\(space.space)"
        var optimistic = todos
        for index in optimistic.indices {
            guard linked || optimistic[index].id == todoID else { continue }
            optimistic[index].spaces?.removeAll { $0.machineID == space.machineID && $0.space == space.space }
            if !linked || optimistic[index].id != todoID {
                optimistic[index].agents.removeAll { $0.source == "space" && $0.via == via }
            }
            if linked && optimistic[index].id == todoID {
                optimistic[index].spaces = (optimistic[index].spaces ?? []) + [space]
            }
        }
        apply(optimistic)
        do {
            if linked {
                try await api.linkTodoSpace(todoID: todoID, machineID: space.machineID, space: space.space)
            } else {
                try await api.unlinkTodoSpace(todoID: todoID, machineID: space.machineID, space: space.space)
            }
            await fetch(api: injected)
            return true
        } catch {
            apply(original)
            if TodosLogic.isLegacySpacesRoute(error) {
                spacesSupported = false
                let upgrade = APIError(status: 404, message: "这台 hub 还不支持整组挂，升级 hub 后再试")
                self.error = upgrade.message
                throw upgrade
            }
            let message = error.shown
            self.error = message
            await fetch(api: injected)
            self.error = message
            throw error
        }
    }

    func link(_ target: TodoLinkTarget, to todoID: String, api: APIClient? = nil) async throws {
        switch target {
        case .space(let project):
            try await setSpace(todoID: todoID, space: project, linked: true, api: api)
        case .agent(let machine, let session, _):
            try await linkAgent(todoID: todoID, machineID: machine, sessionID: session, api: api)
        }
    }

    func unlink(_ target: TodoLinkTarget, from todoID: String, api: APIClient? = nil) async throws {
        switch target {
        case .space(let project):
            try await setSpace(todoID: todoID, space: project, linked: false, api: api)
        case .agent(let machine, let session, _):
            try await unlinkAgent(todoID: todoID, machineID: machine, sessionID: session, api: api)
        }
    }

    /// Creation failure throws; a later link failure returns a message that preserves the created todo.
    func createAndLink(title: String, target: TodoLinkTarget, api: APIClient? = nil) async throws -> String? {
        if target.isSpace && changingSpaces {
            throw APIError(status: 409, message: "正在更新项目，请稍后再试")
        }
        let created = try await create(title: title, api: api)
        do {
            try await link(target, to: created.id, api: api)
            return nil
        } catch {
            return TodosLogic.createdLinkFailure(error, isSpace: target.isSpace)
        }
    }
}
