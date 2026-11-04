import SwiftUI

/// A message typed in a Feishu chat on its way out: on screen the moment 发送 is tapped, sent in the background in the
/// order written, and replaced by Feishu's own copy when that arrives (LarkChatView).
nonisolated struct LarkOutgoing: Identifiable, Equatable, Sendable {
    enum State: Codable, Equatable, Sendable { case sending, sent, failed(String), unconfirmed }

    var id = UUID()
    var text: String
    var encrypted: LarkEncryptedPayload? = nil
    var at = Date()
    var state: State = .sending
    /// The message this answers (id, sender, a one-line summary); nil for a plain message to the chat.
    var replyTo: LarkReplyTo? = nil
    /// With `replyTo`: true goes into its thread, false quotes it in the chat.
    var thread = false
    /// The thread's first message, when `thread` and it is known (a file goes into the thread by its root, #1035).
    var threadRoot: String? = nil
    var attachment: LarkAttachment?
    var messageID: String?
    /// Persisted before the first network write; queued rows remain safely retryable after a restart.
    var attempted = false
    /// Direct file uploads have no server outbox replay contract. An uncertain upload is check-only.
    var canRetry: Bool { state != .sent && (attachment?.kind != .file || state != .unconfirmed) }
    /// Who Feishu notifies (the @ picker), kept for 重发 and 放回输入框.
    var mentions: [LarkMention] = []

    /// What the hub gets as `client_id`: this message's id, unchanged by retries.
    var clientID: String { id.uuidString.lowercased() }

    /// 重发: on its way again, as the same message.
    mutating func retry(at now: Date = .now) {
        state = .sending
        attempted = false
        at = now
    }

    /// Drawn like my other messages: my name and avatar from the last one I sent here.
    func message(like mine: LarkMessage?) -> LarkMessage {
        var message = LarkMessage(id: "outgoing-\(id.uuidString)", position: "", senderID: mine?.senderID, senderName: mine?.senderName ?? "我",
                    senderAvatar: mine?.senderAvatar, senderType: mine?.senderType, isMine: true, type: "TEXT", text: text,
                    createdAt: at, recalled: false, replyCount: 0, replyTo: thread ? nil : replyTo)
        if let encrypted {
            message.olk = LarkEncryption(version: 1, state: "decrypted",
                publicParts: [LarkPart(kind: "text", text: encrypted.cover ?? "")],
                bodyParts: [LarkPart(kind: "text", text: text)])
        }
        return message
    }
}

enum LarkOutbox {
    /// How long a sent message waits for its copy before it goes anyway (the next refresh brings it).
    static let copyWait: TimeInterval = 20

    /// Hands one outgoing message to the hub: a reply when it answers a message, else a plain send to the chat.
    @discardableResult
    static func deliver(_ o: LarkOutgoing, feed: String, api: APIClient) async throws -> LarkSendReceipt {
        if let encrypted = o.encrypted {
            guard encrypted.feedId == feed, !o.thread, o.attachment == nil else { throw LarkOutboxIdentity.Failure.changed }
            return try await api.larkSendEncrypted(encrypted, clientID: o.clientID, replyID: o.replyTo?.id)
        }
        if let attachment = o.attachment {
            return try await api.larkSendAttachment(feed, attachment: attachment, clientID: o.clientID,
                                                    replyTo: o.replyTo?.id, thread: o.thread, threadRoot: o.threadRoot)
        }
        if let reply = o.replyTo {
            return try await api.larkReply(messageID: reply.id, text: o.text, thread: o.thread, clientID: o.clientID, mentions: o.mentions)
        } else {
            return try await api.larkSend(feed, text: o.text, clientID: o.clientID, mentions: o.mentions)
        }
    }

    /// Confirmed IDs replace optimistic rows, including a request whose response was lost.
    /// Only legacy text without a receipt falls back to text/time matching; media waits for its exact copy.
    static func remaining(_ out: [LarkOutgoing], messages: [LarkMessage], now: Date = .now) -> [LarkOutgoing] {
        var mine = messages.filter(\.isMine)
        return out.filter { o in
            // A push can arrive before the request finishes (or after its response was lost).
            // A confirmed client ID is stronger evidence than a local timeout; pending copies are not.
            if let i = mine.firstIndex(where: {
                $0.clientID?.lowercased() == o.clientID && !$0.id.hasPrefix("local:") && $0.localState != "pending" && $0.localState != "failed"
            }) {
                mine.remove(at: i)
                return false
            }
            guard o.state == .sent else { return true }
            if let id = o.messageID {
                // Media has no unique text. Keep its local row until this exact acknowledged copy is visible.
                if let i = mine.firstIndex(where: { $0.id == id && $0.localState != "pending" && $0.localState != "failed" }) {
                    mine.remove(at: i)
                    return false
                }
                return true
            }
            if o.attachment != nil || o.encrypted != nil { return true }
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
        let line = Fmt.plain(m.displayText).split(whereSeparator: \.isNewline).map { $0.trimmingCharacters(in: .whitespaces) }
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
        LarkReplyTo(olk: m.olk, id: m.id, senderName: m.senderName, text: summary(of: m))
    }
}

/// An outgoing message under the chat: my bubble, and beside it a small spinner while it goes or the failed mark
/// (LarkSendMark), like Feishu.
struct LarkOutgoingRow: View {
    var outgoing: LarkOutgoing
    var message: LarkMessage
    var grouped: Bool
    var isGroup: Bool
    var retry: () -> Void
    var edit: () -> Void
    var discard: () -> Void

    var body: some View {
        LarkMessageRow(message: message, grouped: grouped, isGroup: isGroup,
                       sending: LarkSendMark(state: outgoing.state, retry: retry, edit: edit, discard: discard,
                                             canRetry: outgoing.canRetry, canRemoveSent: outgoing.attachment != nil && outgoing.messageID == nil),
                       localAttachment: outgoing.attachment)
            .environment(\.larkReact, nil) // not in Feishu yet: nothing to react to
    }
}

/// How a message of mine on its way stands, drawn beside its bubble: a spinner while it goes, nothing once sent,
/// the failed mark when it did not go out (tap: 重发 / 放回输入框 / 删除).
struct LarkSendMark {
    var state: LarkOutgoing.State
    var retry: () -> Void
    var edit: () -> Void
    var discard: () -> Void
    var canRetry = true
    var canRemoveSent = false
}

struct LarkSendMarkView: View {
    var mark: LarkSendMark

    var body: some View {
        switch mark.state {
        case .sending:
            ProgressView().controlSize(.mini)
                .scaledFrame(width: Theme.Size.iconL, height: Theme.Size.iconL)
                .accessibilityLabel("发送中")
                .accessibilityIdentifier("lark.outgoing.sending")
        case .sent:
            if mark.canRemoveSent {
                Menu {
                    Button(role: .destructive, action: mark.discard) {
                        Label("移除本机记录", systemImage: Theme.Icon.delete).lineLimit(1)
                    }
                } label: {
                    Image(systemName: Theme.Icon.done)
                        .font(Theme.Font.label).foregroundStyle(.secondary)
                        .scaledFrame(width: Theme.Size.iconL, height: Theme.Size.tapTarget)
                        .contentShape(.rect)
                }
                .accessibilityLabel("已发送，管理本机记录")
                .accessibilityHint("仅移除此处的发送记录，不撤回飞书消息")
                .accessibilityIdentifier("lark.outgoing.sentReceipt")
            }
        case .failed, .unconfirmed:
            Menu {
                Button(action: mark.retry) { Label(retryTitle, systemImage: Theme.Icon.retry).lineLimit(1) }
                if mark.state != .unconfirmed {
                    Button(action: mark.edit) { Label("放回输入框", systemImage: Theme.Icon.undo).lineLimit(1) }
                }
                Button(role: .destructive, action: mark.discard) {
                    Label(mark.state == .unconfirmed ? "移除本机记录" : "删除", systemImage: Theme.Icon.delete).lineLimit(1)
                }
            } label: {
                Image(systemName: Theme.Icon.errorFilled)
                    .font(Theme.Font.body)
                    .foregroundStyle(mark.state == .unconfirmed ? Theme.caution : Theme.danger)
                    // As wide as the icon (close to the bubble), as tall as a tap target.
                    .scaledFrame(width: Theme.Size.iconL, height: Theme.Size.tapTarget)
                    .contentShape(.rect)
            }
            .accessibilityLabel(mark.state == .unconfirmed ? "发送结果待确认" : "没发出去，点按重发")
            .accessibilityHint(hint)
            .accessibilityIdentifier(mark.state == .unconfirmed ? "lark.outgoing.unconfirmed" : "lark.outgoing.failed")
        }
    }

    private var hint: String {
        if case .failed(let why) = mark.state { return why }
        return mark.canRetry ? "连接中断，尚未确认发送结果。重试前会先检查已有消息。" : "尚未确认文件是否发出。只检查发送结果，不重复上传。"
    }

    private var retryTitle: String {
        guard mark.state == .unconfirmed else { return "重发" }
        return mark.canRetry ? "检查并重试" : "检查发送结果"
    }
}
