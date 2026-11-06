import SwiftUI

/// The newest replies of a topic group's topics, shown on their cards as the Lark gateway does (#1087). A topic's are read
/// when its card comes on screen, `parallel` topics at a time (a screen holds a dozen cards, and the hub passes each
/// read on to the Lark gateway), and kept with the reply count they were read at: a topic is read again only when its count
/// changes (also while it is being read), or after a read failed: on Retry, or when its card comes on screen again.
@MainActor @Observable
final class LarkTopicReplies {
    /// The newest replies of one topic, oldest first, and the reply count they were read at.
    struct Preview: Equatable {
        var count: Int
        var replies: [LarkMessage]
        var failed = false
    }

    /// Topics read at the same time.
    nonisolated static let parallel = 3
    /// Replies shown on a card.
    nonisolated static let shown = 2

    private(set) var previews: [String: Preview] = [:]
    /// Reads `limit` of a thread's newest messages (the chat page sets it to its hub's `larkThread`); what was wanted
    /// before it is set is read then.
    @ObservationIgnored var fetch: ((_ id: String, _ limit: Int) async throws -> [LarkMessage])? {
        didSet { next() }
    }
    @ObservationIgnored private var waiting: [(id: String, count: Int)] = []
    /// Topics being read, with the count each read is for.
    @ObservationIgnored private var reading: [String: Int] = [:]
    /// A newer count asked for while its topic was being read: read again once that read is in.
    @ObservationIgnored private var again: [String: Int] = [:]

    /// Replies a topic has: Feishu counts them in two places, and either can lag.
    nonisolated static func count(of topic: LarkMessage) -> Int { max(topic.threadReplyCount ?? 0, topic.replyCount) }

    /// The newest `shown` replies of a thread page (its topic left out) and how many replies come before them.
    nonisolated static func preview(of page: [LarkMessage], topic: String, count: Int) -> (replies: [LarkMessage], earlier: Int) {
        let replies = Array(page.filter { $0.id != topic }.suffix(shown))
        return (replies, max(0, count - replies.count))
    }

    /// A reply's line on its topic's card, shown and read out alike: a recalled one only with Keep Recalled Messages on and its content
    /// kept, then marked Recalled, as a chat's row shows it (`LarkMessageDisplay.showsBody`); an encrypted one by its cover,
    /// as the Lark gateway's card.
    static func line(of reply: LarkMessage, display: LarkMessageDisplay) -> String {
        guard display.showsBody(of: reply) else { return String(localized: "Message recalled") }
        let text = reply.olk.map { $0.cached.summary } ?? display.summary(LarkReply.summary(of: reply))
        return reply.recalled ? String(localized: "\(text) · Recalled") : text
    }

    /// A card came on screen or its reply count changed: read the topic's newest replies unless they are known at that count.
    func want(_ topic: LarkMessage) {
        let count = Self.count(of: topic)
        guard count > 0 else { return }
        if let read = reading[topic.id] {
            again[topic.id] = read == count ? nil : count
            return
        }
        if let p = previews[topic.id], p.count == count, !p.failed { return }
        waiting.removeAll { $0.id == topic.id }
        waiting.append((topic.id, count))
        next()
    }

    /// Retry after a read failed.
    func retry(_ topic: LarkMessage) {
        previews[topic.id]?.failed = false
        previews[topic.id]?.count = -1
        want(topic)
    }

    /// What the cards show is out of date as a whole (a messaging source's display settings changed: it sends retained
    /// recalls and edits by them now, #2138; LarkSync.displayStale, which is rare): every topic read before is read
    /// again.
    func stale() {
        for (id, p) in previews where reading[id] == nil && !waiting.contains(where: { $0.id == id }) {
            waiting.append((id, p.count))
        }
        next()
    }

    /// The card went off screen before its turn came: not read for now (nor again after a read under way).
    func drop(_ id: String) {
        waiting.removeAll { $0.id == id }
        again[id] = nil
    }

    private func next() {
        guard let fetch else { return }
        while reading.count < Self.parallel, !waiting.isEmpty {
            let (id, count) = waiting.removeFirst()
            reading[id] = count
            Task {
                do {
                    let page = try await fetch(id, Self.shown + 1)
                    previews[id] = Preview(count: count, replies: Self.preview(of: page, topic: id, count: count).replies)
                } catch {
                    previews[id] = Preview(count: count, replies: previews[id]?.replies ?? [], failed: true)
                }
                reading[id] = nil
                if let newer = again.removeValue(forKey: id) { waiting.append((id, newer)) }
                next()
            }
        }
    }
}
