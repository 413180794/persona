import SwiftUI

extension EnvironmentValues {
    /// 回复 in a message's long-press menu; set by the chat and the thread sheet. Nil: no 回复 item.
    @Entry var larkReply: LarkReplyHandler? = nil
}

/// What 回复 does where the message is shown: the page says whether it is a topic group or a thread sheet, and takes
/// the message to answer (the chat puts up its quote bar, the thread sheet focuses its box).
struct LarkReplyHandler {
    var inTopicGroup = false
    var inThread = false
    var answer: (LarkMessage) -> Void
}

/// The box under a Feishu chat or thread: the text field and the send button, with the quote bar of the message
/// being answered above it. Like Feishu, the send button is only there once there is something to send; until then
/// the field takes the whole row. In a group, typing @ lists the members to mention (LarkMentionPicker).
struct LarkComposer: View {
    @Environment(AppModel.self) private var model
    var placeholder: String
    @Binding var text: String
    var focused: FocusState<Bool>.Binding
    var replyTo: LarkReplyTo? = nil
    var cancelReply: () -> Void = {}
    /// Tell the chat's box from a thread sheet's (UI tests; the chat stays behind the sheet). The field's placeholder
    /// goes once it holds text, so tests find it by its id.
    var boxID = "lark.box"
    var sendID = "lark.send"
    var attachmentPicker: ((Bool) -> Void)? = nil
    var canAddAttachments = true
    var hasAttachments = false
    var preparingAttachment = false
    /// The group whose members @ offers; nil in a 1:1 chat (no picker).
    var mentionFeed: String? = nil
    /// My user id there (I am not offered).
    var me: String? = nil
    /// Those picked so far; what is sent keeps the ones whose `@name` is still in the text (LarkMentions).
    var mentions: Binding<[LarkMention]> = .constant([])
    var send: () -> Void
    /// The members matching the @ being typed, and whether the hub answered for it (an older hub without the route
    /// does not: no picker, the box works as before).
    @State private var members: [LarkMember] = []
    @State private var membersAnswered = false
    /// Where the cursor is: an @ is looked up, and a picked name goes in, there (not only at the end).
    @State private var selection: TextSelection?

    /// The @ being typed before the cursor, in a group. Before the field reports a cursor it is the end of the text;
    /// while a range is selected no @ is being typed.
    private var typing: LarkMentions.Typing? {
        guard mentionFeed != nil else { return nil }
        guard let selection else { return LarkMentions.typing(in: text) }
        guard case .selection(let r) = selection.indices, r.isEmpty else { return nil }
        let at = min(r.lowerBound, text.endIndex) // the text may have changed since (a send, a draft put back)
        return LarkMentions.typing(in: text, cursor: text.distance(from: text.startIndex, to: at))
    }

    private var mentionQuery: String? { typing?.query }

    private var showsSend: Bool { Self.canSend(text) || (hasAttachments && replyTo == nil) }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if let q = mentionQuery, membersAnswered, !members.isEmpty || LarkMentions.offersEveryone(q) {
                LarkMentionPicker(query: q, members: members) { m in
                    let picked = LarkMentions.insert(m, into: text, cursor: typing?.end)
                    text = picked.text
                    selection = TextSelection(insertionPoint: text.index(text.startIndex, offsetBy: picked.cursor))
                    mentions.wrappedValue.append(m)
                }
                .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            if let replyTo {
                LarkReplyBar(reply: replyTo, cancel: cancelReply)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            HStack(alignment: .bottom, spacing: Theme.Space.s) {
                if let attachmentPicker {
                    Menu {
                        Button { attachmentPicker(true) } label: { Label("照片", systemImage: Theme.Icon.photos).lineLimit(1) }
                        Button { attachmentPicker(false) } label: { Label("文件", systemImage: Theme.Icon.file).lineLimit(1) }
                    } label: {
                        Image(systemName: Theme.Icon.add).font(Theme.Font.heading)
                            .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                    }
                    .disabled(preparingAttachment || !canAddAttachments)
                    .accessibilityLabel("添加附件")
                }
                TextField(placeholder, text: $text, selection: $selection, axis: .vertical)
                    .lineLimit(1...5)
                    .focused(focused)
                    .accessibilityIdentifier(boxID)
                    .padding(.horizontal, Theme.Space.l)
                    .padding(.vertical, Theme.Space.s)
                    .surface(interactive: true, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                if showsSend {
                    Button(action: send) {
                        Image(systemName: Theme.Icon.send).font(Theme.Font.heading)
                            .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                    }
                    .themedButton(prominent: true)
                    .clipShape(.circle)
                    .disabled(preparingAttachment)
                    .accessibilityLabel("发送")
                    .accessibilityIdentifier(sendID)
                    .transition(.scale.combined(with: .opacity))
                }
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.bottom, Theme.Space.s)
        .animation(Theme.Motion.standard, value: replyTo)
        .animation(Theme.Motion.standard, value: showsSend)
        .animation(Theme.Motion.standard, value: mentionQuery == nil)
        .task(id: mentionQuery) { await lookUp(mentionQuery) }
    }

    /// Asks the hub for members a moment after the last keystroke after @.
    private func lookUp(_ query: String?) async {
        guard let query, let feed = mentionFeed else {
            membersAnswered = false
            return
        }
        try? await Task.sleep(for: .milliseconds(200))
        guard !Task.isCancelled, let api = model.api else { return }
        do {
            let found = try await api.larkMembers(feed, query: query).members
            members = found.filter { $0.userID != me }
            membersAnswered = true
        } catch {
            guard !error.isCancellation else { return }
            members = []
            membersAnswered = false
        }
    }

    /// Words to send: blank lines and spaces alone are not.
    static func canSend(_ text: String) -> Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }
}

/// 回复 某某 over the box: who is answered, one line of what they wrote, and a button to drop it.
struct LarkReplyBar: View {
    var reply: LarkReplyTo
    var cancel: () -> Void

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            Capsule().fill(LarkColor.blue).frame(width: Theme.Size.ruleWide)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text("回复 \(reply.senderName)").font(Theme.Font.labelMedium).foregroundStyle(LarkColor.blue).lineLimit(1)
                Text(reply.text).font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: cancel) {
                Image(systemName: Theme.Icon.clear).font(Theme.Font.heading).foregroundStyle(.tertiary)
                    .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("取消回复")
        }
        .fixedSize(horizontal: false, vertical: true)
        .padding(.leading, Theme.Space.m)
        .surface(in: .rect(cornerRadius: Theme.Radius.control, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("lark.replyBar")
    }
}
