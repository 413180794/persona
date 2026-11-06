import Foundation

/// Where a phone tool's result goes: the group's socket (`LiveConnection`), or a fake in the tests.
@MainActor protocol ToolResultLink: AnyObject {
    var canSend: Bool { get }
    func send(_ object: [String: Any])
}

extension LiveConnection: ToolResultLink {
    /// `send` drops a message while the socket is not up (the in-app demo hub counts as connected).
    var canSend: Bool { state == .connected }
}

/// A phone tool's result that could not go out yet (#4456): the user tapped Allow while the socket was down (the app had
/// just come back from the background), and `LiveConnection.send` would have dropped it, leaving the agent waiting for
/// the hub to give up. It is kept and sent once the group connects again; the hub matches it to the call by `call_id`
/// whatever connection it comes on. Only tool results are kept, a few per group, and only as long as the hub still waits
/// for them (`phoneConfirmWait` in server/internal/hub/hub.go, the longest wait of a call).
@MainActor final class ToolResultOutbox {
    /// The hub's longest wait for a call's result: past it, nobody takes the result.
    static let hubWait: TimeInterval = 30 * 60
    /// Per group: results of calls still waiting; more would be calls the hub has long given up on.
    static let most = 16

    private struct Held {
        var callID: String
        var message: [String: Any]
        var at: Date
    }
    private var held: [String: [Held]] = [:]

    /// Sends the result now if `link` can, else keeps it for `flush`.
    func send(_ message: [String: Any], callID: String, group: String, on link: (any ToolResultLink)?, now: Date = .now) {
        if let link, link.canSend {
            link.send(message)
            return
        }
        var list = held[group, default: []].filter { $0.callID != callID }
        list.append(Held(callID: callID, message: message, at: now))
        if list.count > Self.most { list.removeFirst(list.count - Self.most) }
        held[group] = list
    }

    /// The group connected: what it kept goes out, oldest first, except what the hub no longer waits for.
    func flush(group: String, on link: any ToolResultLink, now: Date = .now) {
        guard link.canSend, let list = held.removeValue(forKey: group) else { return }
        for h in list where now.timeIntervalSince(h.at) < Self.hubWait { link.send(h.message) }
    }

    /// A group forgotten: its results have nowhere to go.
    func forget(groups: Set<String>) {
        for g in groups { held[g] = nil }
    }

    /// How many results a group keeps (the tests).
    func count(_ group: String) -> Int { held[group]?.count ?? 0 }
}
