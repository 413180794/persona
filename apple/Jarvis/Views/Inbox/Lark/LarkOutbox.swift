import SwiftUI

/// A message typed in a Feishu chat on its way out: on screen the moment 发送 is tapped, sent in the background in the
/// order written, and replaced by Feishu's own copy when that arrives (LarkChatView).
struct LarkOutgoing: Identifiable, Equatable {
    enum State: Equatable { case sending, sent, failed(String) }

    var id = UUID()
    var text: String
    var at = Date()
    var state: State = .sending

    /// What the hub gets as `client_id`: this message's id, unchanged by retries.
    var clientID: String { id.uuidString.lowercased() }

    /// 重发: on its way again, as the same message.
    mutating func retry(at now: Date = .now) {
        state = .sending
        at = now
    }

    /// Drawn like my other messages: my name and avatar from the last one I sent here.
    func message(like mine: LarkMessage?) -> LarkMessage {
        LarkMessage(id: "outgoing-\(id.uuidString)", position: "", senderID: mine?.senderID, senderName: mine?.senderName ?? "我",
                    senderAvatar: mine?.senderAvatar, senderType: mine?.senderType, isMine: true, type: "TEXT", text: text,
                    createdAt: at, recalled: false, replyCount: 0)
    }
}

enum LarkOutbox {
    /// How long a sent message waits for its copy before it goes anyway (the next refresh brings it).
    static let copyWait: TimeInterval = 20

    /// What is still shown: a sent message goes once the chat has my message with its text (written since it was
    /// sent), or after `copyWait`; sending and failed ones stay.
    static func remaining(_ out: [LarkOutgoing], messages: [LarkMessage], now: Date = .now) -> [LarkOutgoing] {
        var mine = messages.filter(\.isMine)
        return out.filter { o in
            guard o.state == .sent else { return true }
            if now.timeIntervalSince(o.at) > copyWait { return false }
            let text = o.text.trimmingCharacters(in: .whitespacesAndNewlines)
            // One copy answers one message (the same words sent twice need two).
            if let i = mine.firstIndex(where: {
                $0.text.trimmingCharacters(in: .whitespacesAndNewlines) == text && ($0.createdAt ?? now) >= o.at.addingTimeInterval(-60)
            }) {
                mine.remove(at: i)
                return false
            }
            return true
        }
    }
}

/// An outgoing message under the chat: the bubble, faded while it goes, and below it 发送中 or what went wrong.
struct LarkOutgoingRow: View {
    var outgoing: LarkOutgoing
    var message: LarkMessage
    var grouped: Bool
    var isGroup: Bool
    var retry: () -> Void
    var edit: () -> Void
    var discard: () -> Void

    var body: some View {
        VStack(alignment: .trailing, spacing: Theme.Space.xs) {
            LarkMessageRow(message: message, grouped: grouped, isGroup: isGroup)
                .environment(\.larkReact, nil) // not in Feishu yet: nothing to react to
                .opacity(outgoing.state == .sending ? 0.6 : 1)
            switch outgoing.state {
            case .sending:
                HStack(spacing: Theme.Space.xs) {
                    ProgressView().controlSize(.mini)
                    Text("发送中").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
                .accessibilityElement(children: .combine)
            case .sent:
                EmptyView()
            case .failed(let why):
                Menu {
                    Button(action: retry) { Label("重发", systemImage: Theme.Icon.retry) }
                    Button(action: edit) { Label("放回输入框", systemImage: Theme.Icon.undo) }
                    Button(role: .destructive, action: discard) { Label("删除", systemImage: Theme.Icon.delete) }
                } label: {
                    Label("没发出去，点按重发", systemImage: Theme.Icon.errorFilled)
                        .font(Theme.Font.label).foregroundStyle(Theme.danger).lineLimit(1)
                        .scaledFrame(minHeight: Theme.Size.tapTarget)
                }
                .accessibilityHint(why)
            }
        }
        .accessibilityIdentifier("lark.outgoing")
    }
}
