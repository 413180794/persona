import SwiftUI

/// A message typed in a Feishu chat on its way out: on screen the moment 发送 is tapped, sent in the background in the
/// order written, and replaced by Feishu's own copy when that arrives (LarkChatView).
struct LarkOutgoing: Identifiable, Equatable {
    enum State: Equatable { case sending, sent, failed(String) }

    var id = UUID()
    var text: String
    var at = Date()
    var state: State = .sending
    /// The message this answers (id, sender, a one-line summary); nil for a plain message to the chat.
    var replyTo: LarkReplyTo? = nil
    /// With `replyTo`: true goes into its thread, false quotes it in the chat.
    var thread = false

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
                    createdAt: at, recalled: false, replyCount: 0, replyTo: thread ? nil : replyTo)
    }
}

enum LarkOutbox {
    /// How long a sent message waits for its copy before it goes anyway (the next refresh brings it).
    static let copyWait: TimeInterval = 20

    /// Hands one outgoing message to the hub: a reply when it answers a message, else a plain send to the chat.
    static func deliver(_ o: LarkOutgoing, feed: String, api: APIClient) async throws {
        if let reply = o.replyTo {
            try await api.larkReply(messageID: reply.id, text: o.text, thread: o.thread, clientID: o.clientID)
        } else {
            try await api.larkSend(feed, text: o.text, clientID: o.clientID)
        }
    }

    /// What is still shown: a sent message goes once the chat has my message with its text (written since it was
    /// sent), or after `copyWait`; sending and failed ones stay.
    static func remaining(_ out: [LarkOutgoing], messages: [LarkMessage], now: Date = .now) -> [LarkOutgoing] {
        var mine = messages.filter(\.isMine)
        return out.filter { o in
            guard o.state == .sent else { return true }
            if now.timeIntervalSince(o.at) > copyWait { return false }
            let text = o.text.trimmingCharacters(in: .whitespacesAndNewlines)
            // One copy answers one message (the same words sent twice need two). A quoted reply's copy quotes the
            // same message when the hub says what it quotes.
            if let i = mine.firstIndex(where: {
                $0.text.trimmingCharacters(in: .whitespacesAndNewlines) == text && ($0.createdAt ?? now) >= o.at.addingTimeInterval(-60)
                    && (o.replyTo == nil || o.thread || $0.replyTo == nil || $0.replyTo?.id == o.replyTo?.id)
            }) {
                mine.remove(at: i)
                return false
            }
            return true
        }
    }
}

/// 回复 on a message (long press): where the answer goes.
enum LarkReply {
    enum Route: Equatable {
        /// A quote bar over the chat's box; the message goes to the chat quoting it.
        case quote
        /// A topic group lists threads: open this one's sheet with its box focused (not a new topic at the bottom).
        case openThread
        /// Already in a thread sheet: its box, the reply goes into the thread.
        case threadBox
    }

    /// Nil when there is nothing to answer (system lines, recalled messages, ones not in Feishu yet).
    static func route(for m: LarkMessage, inTopicGroup: Bool, inThread: Bool) -> Route? {
        guard !m.isSystem, !m.recalled, !m.id.isEmpty, !m.id.hasPrefix("outgoing-") else { return nil }
        if inThread { return .threadBox }
        return inTopicGroup ? .openThread : .quote
    }

    /// What the quote bar and the sending row show of the message answered: one line of its text.
    static func summary(of m: LarkMessage) -> String {
        let line = Fmt.plain(m.text).split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }.joined(separator: " ")
        if !line.isEmpty { return line }
        switch m.type.uppercased() {
        case "IMAGE": return "[图片]"
        case "FILE": return "[文件]"
        case "MEDIA": return "[视频]"
        case "AUDIO": return "[语音]"
        case "STICKER": return "[贴纸]"
        default: return "[消息]"
        }
    }

    /// The quote an outgoing reply carries.
    static func quote(_ m: LarkMessage) -> LarkReplyTo {
        LarkReplyTo(id: m.id, senderName: m.senderName, text: summary(of: m))
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
