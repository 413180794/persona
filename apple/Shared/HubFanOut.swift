import Foundation

/// Asking every group's hub the same thing (docs/multi-hub.md): all at once, so a hub out of reach (its requests wait
/// out their 30 s) holds back none of the others (#4705: 机器's numbers, 订阅额度, the watch, the Live Activity and the
/// pins asked one hub after another).
nonisolated enum HubFanOut {
    /// The groups worth asking on a poll: those whose link is up. With none up, all of them (nothing to hold back, and
    /// a hub may answer requests while its socket is still coming up).
    static func asked(_ groups: [String], up: (String) -> Bool) -> [String] {
        let live = groups.filter(up)
        return live.isEmpty ? groups : live
    }

    /// `fetch` for each group at once; `each` gets every answer as it comes, on the caller's actor, in no fixed order.
    /// A failed group's answer is its error. Cancelling the caller cancels them all.
    static func run<T: Sendable>(_ groups: [String], isolation: isolated (any Actor)? = #isolation,
                                 fetch: @escaping @Sendable (String) async throws -> T,
                                 each: (String, Result<T, Error>) -> Void) async {
        await withTaskGroup(of: (String, Result<T, Error>).self) { tasks in
            for g in groups {
                tasks.addTask {
                    do { return (g, .success(try await fetch(g))) } catch { return (g, .failure(error)) }
                }
            }
            for await (g, result) in tasks { each(g, result) }
        }
    }
}
