import Foundation

/// Survives leaving and reopening the chat, isolated by Hub credential and conversation.
/// Serializes network sends only; waiting for echoed messages never blocks the next send.
@MainActor @Observable
final class LarkChatOutbox {
    private static var chats: [ServerConfig: [String: LarkChatOutbox]] = [:]
    static func shared(config: ServerConfig, feed: String) -> LarkChatOutbox {
        if let existing = chats[config]?[feed] { return existing }
        let result = LarkChatOutbox()
        chats[config, default: [:]][feed] = result
        return result
    }

    var items: [LarkOutgoing] = []
    @ObservationIgnored private var chain: Task<Void, Never>?

    func deliver(_ id: UUID, using api: APIClient, feed: String, refresh: @escaping () async -> Void) {
        let before = chain
        chain = Task {
            await before?.value
            guard let outgoing = items.first(where: { $0.id == id }), outgoing.state == .sending else { return }
            do {
                let receipt = try await LarkOutbox.deliver(outgoing, feed: feed, api: api)
                if let i = items.firstIndex(where: { $0.id == id }) {
                    items[i].messageID = receipt.messageID
                    items[i].state = .sent
                }
                Task {
                    await refresh()
                    try? await Task.sleep(for: .seconds(2))
                    await refresh()
                    try? await Task.sleep(for: .seconds(LarkOutbox.copyWait))
                    await refresh()
                }
            } catch {
                if let i = items.firstIndex(where: { $0.id == id }) {
                    items[i].state = .failed(error.shown ?? error.localizedDescription)
                }
                Haptics.error()
            }
        }
    }

    func settle(_ messages: [LarkMessage]) {
        let left = LarkOutbox.remaining(items, messages: messages)
        if left != items { items = left }
    }

    /// lark-connector may publish its local pending copy before returning the receipt. Draw our row only once.
    func hidesEcho(_ message: LarkMessage) -> Bool {
        // lark-connector's unconfirmed local rows must not become ordinary sent bubbles after a local discard.
        if message.localState == "pending" || message.localState == "failed" || message.id.hasPrefix("local:") { return true }
        return message.isMine && items.contains { message.id == "local:" + $0.clientID || message.clientID?.lowercased() == $0.clientID }
    }
}
