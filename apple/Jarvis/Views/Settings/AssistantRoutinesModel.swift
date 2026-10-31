import SwiftUI

/// What the assistant's pages (定时任务, 记忆) need from the app: the hub, error reporting, toasts and opening a
/// conversation. `live` is AppModel; tests pass a FakeHub's client and record the rest.
struct AssistantDeps {
    var api: () -> APIClient?
    var report: (Error) -> Void
    var toast: (Toast) -> Void
    /// Show a conversation in the Jarvis tab (a task that was run now).
    var open: (SessionKey) -> Void

    static var live: AssistantDeps {
        let model = AppModel.shared
        return AssistantDeps(api: { model.api }, report: { model.show($0) }, toast: { model.toast = $0 },
                             open: { key in model.selectedTab = .assistant; model.assistantCurrent = key })
    }
}

/// 定时任务's state and everything the page does with the hub (MVVM: ARCH-PLAN.md). The view draws what is here and
/// calls its methods, so tests run it against a `FakeHub`. One list: what repeats (every day, every N minutes) and
/// the one-offs Jarvis scheduled from a conversation.
@Observable final class AssistantRoutinesModel {
    typealias Deps = AssistantDeps
    typealias Item = APIClient.ScheduledTask

    private(set) var tasks: [Item] = []
    private(set) var lastRuns: [APIClient.ScheduleRun] = []
    private(set) var loaded = false
    /// Why the list could not be read; the page shows it only when it has none to show.
    private(set) var error: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) { self.deps = deps }

    /// Every day / every N minutes, in the hub's order.
    var repeating: [Item] { tasks.filter { $0.repeats != .once } }
    /// One-offs, soonest first.
    var oneOff: [Item] { tasks.filter { $0.repeats == .once }.sorted { ($0.at ?? .distantFuture) < ($1.at ?? .distantFuture) } }

    var upcoming: [AssistantUpcoming] { Array(AssistantUpcoming.merge(tasks).prefix(4)) }

    func load() async {
        guard let api = deps.api() else { return }
        async let h = try? api.scheduleRuns()
        do {
            tasks = try await api.scheduledTasks()
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        lastRuns = await h ?? []
        loaded = true
    }

    /// Adds `t` (no id yet) or saves it; returns it as saved. Optimistic: a toggle does not bounce back while saving;
    /// a failure reports and reloads what the hub has.
    func save(_ t: Item) async -> Item? {
        guard let api = deps.api() else { return nil }
        if let i = tasks.firstIndex(where: { $0.id == t.id && !t.id.isEmpty }) {
            withAnimation(Theme.Motion.standard) { tasks[i] = t }
        }
        do {
            let saved = try await api.saveTask(t)
            if let i = tasks.firstIndex(where: { $0.id == saved.id }) { tasks[i] = saved } else { tasks.append(saved) }
            return saved
        } catch {
            deps.report(error)
            if let fresh = try? await api.scheduledTasks() { tasks = fresh }
            return nil
        }
    }

    func delete(_ t: Item) async {
        guard let api = deps.api(), !t.id.isEmpty else { return }
        withAnimation { tasks.removeAll { $0.id == t.id } }
        do {
            try await api.deleteTask(t.id)
            deps.toast(Toast("已删除「\(AssistantScheduleText.title(t))」", style: .success))
        } catch {
            deps.report(error)
            if let fresh = try? await api.scheduledTasks() { tasks = fresh }
        }
    }

    /// 立即运行: opens the conversation it went to. A one-off is done after it (the hub drops it from the list).
    func run(_ t: Item) async {
        guard let api = deps.api() else { return }
        if t.repeats == .once { withAnimation { tasks.removeAll { $0.id == t.id } } }
        do {
            let key = try await api.runTask(t.id)
            Haptics.success()
            deps.open(key)
        } catch {
            deps.report(error)
            if let fresh = try? await api.scheduledTasks() { tasks = fresh }
        }
    }
}

/// 记忆: the assistant's memory files, newest first, and deleting one.
@Observable final class AssistantMemoryModel {
    private(set) var files: [APIClient.MemoryFile] = []
    private(set) var loaded = false
    private(set) var error: String?
    @ObservationIgnored private let deps: AssistantDeps

    init(deps: AssistantDeps = .live) { self.deps = deps }

    func load() async {
        guard let api = deps.api() else { return }
        do {
            files = try await api.assistantMemory()
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        loaded = true
    }

    func delete(_ name: String) async {
        guard let api = deps.api() else { return }
        do {
            try await api.deleteMemory(name: name)
            Haptics.success()
            await load()
        } catch {
            deps.report(error)
        }
    }
}
