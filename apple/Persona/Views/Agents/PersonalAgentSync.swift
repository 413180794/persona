import Foundation

/// The hub's `{"type":"agents","session_ids":[…]}` events: lists and open sessions re-pull on them. An empty list
/// means "something changed" and wakes every listener.
@MainActor
final class PersonalAgentSync {
    static let shared = PersonalAgentSync()

    private var listeners: [UUID: (id: String?, cont: AsyncStream<Void>.Continuation)] = [:]

    /// Fires on every change (`id` nil), or on those touching one session. Bursts collapse into one.
    func changes(of id: String? = nil) -> AsyncStream<Void> {
        AsyncStream(bufferingPolicy: .bufferingNewest(1)) { cont in
            let key = UUID()
            listeners[key] = (id, cont)
            cont.onTermination = { _ in Task { @MainActor in PersonalAgentSync.shared.listeners[key] = nil } }
        }
    }

    func received(_ ids: [String]) {
        for l in listeners.values where l.id == nil || ids.isEmpty || ids.contains(l.id!) { l.cont.yield() }
    }
}
