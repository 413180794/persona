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
/// being answered above it.
struct LarkComposer: View {
    var placeholder: String
    @Binding var text: String
    var focused: FocusState<Bool>.Binding
    var replyTo: LarkReplyTo? = nil
    var cancelReply: () -> Void = {}
    /// Tells the chat's box from a thread sheet's (UI tests; the chat stays behind the sheet).
    var sendID = "lark.send"
    var send: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            if let replyTo {
                LarkReplyBar(reply: replyTo, cancel: cancelReply)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
            }
            HStack(alignment: .bottom, spacing: Theme.Space.s) {
                TextField(placeholder, text: $text, axis: .vertical)
                    .lineLimit(1...5)
                    .focused(focused)
                    .padding(.horizontal, Theme.Space.l)
                    .padding(.vertical, Theme.Space.s)
                    .surface(interactive: true, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                Button(action: send) {
                    Image(systemName: Theme.Icon.send).font(Theme.Font.heading)
                        .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                }
                .themedButton(prominent: true)
                .clipShape(.circle)
                .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                .accessibilityLabel("发送")
                .accessibilityIdentifier(sendID)
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.bottom, Theme.Space.s)
        .animation(Theme.Motion.standard, value: replyTo)
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
