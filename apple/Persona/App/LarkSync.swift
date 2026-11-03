import Foundation

/// Keeps Feishu current while the app is open and online. Push: the hub relays lark-connector's realtime events as
/// `lark` (which conversations changed); the inbox and an open chat refresh on them instead of waiting for a poll.
/// Pull: the latest page of each changed conversation goes into the cache its chat paints from, so opening it shows
/// the new messages at once; on (re)connect the first page of the inbox is checked the same way.
@MainActor
final class LarkSync {
    static let shared = LarkSync()

    static let concurrency = 2
    /// One conversation is fetched at most this often, however busy it is (its last change is still fetched).
    static let minGap: Duration = .seconds(3)
    /// On (re)connect, how many of the most recent conversations are brought up to date.
    static let reconnectCount = 20

    private var listeners: [UUID: (feed: String?, cont: AsyncStream<Void>.Continuation)] = [:]
    /// Conversations from the inbox list: a topic group needs its own kind of page, and only listed ones are fetched.
    private var known: [String: LarkConversation] = [:]
    /// The `updatedAt` each conversation had when its page was cached; kept across launches.
    private var fetched: [String: Date]
    private var queue: [String] = []
    private var running = Set<String>()
    private var cooling = Set<String>() // fetched within minGap
    private var dirty = Set<String>()   // changed again while running / cooling
    private let saver = Debouncer(.seconds(2))
    private static let stateName = "lark-sync"

    private init() {
        fetched = StateCache.load(Self.stateName, as: [String: Date].self) ?? [:]
    }

    /// Fires on every relayed change (`feed` nil), or on those touching one conversation. Bursts collapse into one.
    func changes(of feed: String? = nil) -> AsyncStream<Void> {
        AsyncStream(bufferingPolicy: .bufferingNewest(1)) { cont in
            let id = UUID()
            listeners[id] = (feed, cont)
            cont.onTermination = { _ in Task { @MainActor in LarkSync.shared.listeners[id] = nil } }
        }
    }

    /// The chat caches were cleared: fetch the recent conversations again.
    func forget() {
        fetched = [:]
        reconnected()
    }

    /// The inbox learned its conversations.
    func know(_ convs: [LarkConversation]) {
        for c in convs { known[c.id] = c }
    }

    /// The hub's `lark` event.
    func received(_ feedIDs: [String]) {
        for l in listeners.values where l.feed == nil || feedIDs.contains(l.feed!) { l.cont.yield() }
        enqueue(feedIDs.filter { known[$0] != nil })
    }

    /// The socket came (back) up: whatever happened while away is pulled now.
    func reconnected() {
        for l in listeners.values where l.feed == nil { l.cont.yield() }
        Task {
            guard let api = AppModel.shared.api, let r = try? await api.larkConversations() else { return }
            know(r.conversations)
            let stale = r.conversations.prefix(Self.reconnectCount).filter { c in
                guard let at = c.updatedAt else { return false }
                return fetched[c.id].map { $0 < at } ?? true
            }
            enqueue(stale.map(\.id))
        }
    }

    private func enqueue(_ ids: [String]) {
        for id in ids where !isOpen(id) {
            if running.contains(id) || cooling.contains(id) { dirty.insert(id) } else if !queue.contains(id) { queue.append(id) }
        }
        pump()
    }

    /// An open chat keeps itself current (and caches what it shows).
    private func isOpen(_ id: String) -> Bool { listeners.values.contains { $0.feed == id } }

    private func pump() {
        while running.count < Self.concurrency, !queue.isEmpty {
            let id = queue.removeFirst()
            guard let conv = known[id], AppModel.shared.live.state == .connected, let api = AppModel.shared.api else { continue }
            running.insert(id)
            Task {
                if let r = try? await api.larkMessages(id, topic: conv.isTopic ?? false), r.hasNewer != true {
                    StateCache.save("lark-chat-\(id)", Array(r.messages.suffix(80)))
                    fetched[id] = .now
                    let snapshot = fetched
                    saver.run { StateCache.save(Self.stateName, snapshot) }
                }
                running.remove(id)
                cooling.insert(id)
                pump()
                try? await Task.sleep(for: Self.minGap)
                cooling.remove(id)
                if dirty.remove(id) != nil { enqueue([id]) }
            }
        }
    }
}
