import SwiftUI

/// What the assistant's pages (定时任务, 跟进, 记忆) need from the app: the hub, error reporting, toasts and opening a
/// conversation. `live` is AppModel; tests pass a FakeHub's client and record the rest.
struct AssistantDeps {
    var api: () -> APIClient?
    var report: (Error) -> Void
    var toast: (Toast) -> Void
    /// Show a conversation in the Jarvis tab (a follow-up that was run now).
    var open: (SessionKey) -> Void

    static var live: AssistantDeps {
        let model = AppModel.shared
        return AssistantDeps(api: { model.api }, report: { model.show($0) }, toast: { model.toast = $0 },
                             open: { key in model.selectedTab = .assistant; model.assistantCurrent = key })
    }
}

/// Follow-ups Jarvis scheduled for itself: run one now or cancel it, both shown at once and reloaded on failure.
/// Shared by 定时任务 (the first five) and 已安排的跟进 (all of them).
@Observable final class AssistantFollowups {
    private(set) var list: [APIClient.Followup] = []
    @ObservationIgnored private let deps: AssistantDeps

    init(deps: AssistantDeps) { self.deps = deps }

    /// Soonest first. Throws what the hub said, for a page that shows it.
    func load() async throws {
        guard let api = deps.api() else { return }
        list = try await api.followups().sorted { $0.at < $1.at }
    }

    func run(_ f: APIClient.Followup) async {
        guard let api = deps.api() else { return }
        withAnimation { list.removeAll { $0.id == f.id } }
        do {
            let key = try await api.runFollowup(f.id)
            Haptics.success()
            deps.open(key)
        } catch {
            deps.report(error)
            try? await load()
        }
    }

    func cancel(_ f: APIClient.Followup) async {
        guard let api = deps.api() else { return }
        withAnimation { list.removeAll { $0.id == f.id } }
        do {
            try await api.cancelFollowup(f.id)
            Haptics.success()
        } catch {
            deps.report(error)
            try? await load()
        }
    }
}

/// 定时任务's state and everything the page does with the hub (MVVM: ARCH-PLAN.md). The view draws what is here and
/// calls its methods, so tests run it against a `FakeHub`.
@Observable final class AssistantRoutinesModel {
    typealias Deps = AssistantDeps

    private(set) var routines: [APIClient.Routine] = []
    let followupList: AssistantFollowups
    var followups: [APIClient.Followup] { followupList.list }
    private(set) var lastRuns: [APIClient.ScheduleRun] = []
    private(set) var loaded = false
    /// Why the routines could not be read; the page shows it only when it has none to show.
    private(set) var error: String?
    @ObservationIgnored private let deps: Deps

    init(deps: Deps = .live) {
        self.deps = deps
        followupList = AssistantFollowups(deps: deps)
    }

    var upcoming: [AssistantUpcoming] {
        Array(AssistantUpcoming.merge(routines: routines, followups: followups).prefix(4))
    }

    func load() async {
        guard let api = deps.api() else { return }
        async let f: Void? = try? followupList.load()
        async let h = try? api.scheduleRuns()
        do {
            routines = try await api.routines()
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        _ = await f
        lastRuns = await h ?? []
        loaded = true
    }

    /// Replaces the hub's whole list with `r` added or updated; returns `r` as saved (new ones get their id here).
    /// Optimistic: the toggle does not bounce back while saving; a failure reloads what the hub has.
    func save(_ r: APIClient.Routine) async -> APIClient.Routine? {
        guard let api = deps.api() else { return nil }
        var list = routines
        if let i = list.firstIndex(where: { $0.id == r.id && !r.id.isEmpty }) { list[i] = r } else { list.append(r) }
        withAnimation(Theme.Motion.standard) { routines = list }
        do {
            let saved = try await api.setRoutines(list)
            routines = saved
            return r.id.isEmpty ? saved.last : saved.first { $0.id == r.id }
        } catch {
            deps.report(error)
            if let fresh = try? await api.routines() { routines = fresh }
            return nil
        }
    }

    func runFollowup(_ f: APIClient.Followup) async { await followupList.run(f) }
    func cancel(_ f: APIClient.Followup) async { await followupList.cancel(f) }

    func delete(_ r: APIClient.Routine) async {
        guard let api = deps.api(), !r.id.isEmpty else { return }
        do {
            routines = try await api.setRoutines(routines.filter { $0.id != r.id })
            deps.toast(Toast("已删除「\(r.title)」", style: .success))
        } catch {
            deps.report(error)
        }
    }
}

/// 已安排的跟进: every follow-up, or why they could not be read.
@Observable final class AssistantFollowupsModel {
    let followups: AssistantFollowups
    private(set) var loaded = false
    private(set) var error: String?

    init(deps: AssistantDeps = .live) { followups = AssistantFollowups(deps: deps) }

    func load() async {
        do {
            try await followups.load()
            error = nil
        } catch {
            self.error = error.shown ?? self.error
        }
        loaded = true
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
