import SwiftUI

/// The box at the bottom of a connector's conversation page (Telegram's chat, GitHub's thread): what is answered over
/// the field, send once there are words, and while the hub waits for the user to allow it on the phone
/// (connector_gate), a spinner and the line saying so.
struct ThreadComposer: View {
    var placeholder: String
    @Binding var text: String
    @Binding var selection: TextSelection?
    var focused: FocusState<Bool>.Binding
    /// What the reply answers, over the field ("回复 <who>", its words); nil: nothing.
    var quote: (heading: String, summary: String)?
    var sending: Bool
    var cancelQuote: () -> Void
    var send: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            HStack(alignment: .bottom, spacing: Theme.Space.s) {
                VStack(alignment: .leading, spacing: 0) {
                    if let quote {
                        QuoteBarRow(heading: quote.heading, summary: quote.summary, lines: 1, tint: LarkColor.blue,
                                    removeLabel: "取消回复", remove: cancelQuote)
                            .padding(.leading, Theme.Space.l)
                            .padding(.trailing, Theme.Space.xs)
                            .padding(.top, Theme.Space.s)
                    }
                    TextField(placeholder, text: $text, selection: $selection, axis: .vertical)
                        .lineLimit(1...5)
                        .focused(focused)
                        .disabled(sending)
                        .accessibilityIdentifier("thread.box")
                        .padding(.horizontal, Theme.Space.l)
                        .padding(.vertical, Theme.Space.s)
                }
                .surface(interactive: true, in: .rect(cornerRadius: Theme.Radius.bubble, style: .continuous))
                if sending {
                    ProgressView().scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                } else if LarkComposer.canSend(text) {
                    Button(action: send) {
                        Image(systemName: Theme.Icon.send).font(Theme.Font.heading)
                            .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                    }
                    .themedButton(prominent: true)
                    .clipShape(.circle)
                    .accessibilityLabel("发送")
                    .accessibilityIdentifier("thread.send")
                    .transition(.scale.combined(with: .opacity))
                }
            }
            if sending {
                Text("在手机上弹出的确认卡片里点「允许」后发出").font(Theme.Font.caption).foregroundStyle(.secondary)
                    .padding(.horizontal, Theme.Space.l)
            }
        }
        .padding(.horizontal, Theme.Space.m)
        .padding(.bottom, Theme.Space.s)
        .animation(Theme.Motion.standard, value: quote.map { $0.heading + "\n" + $0.summary })
        .animation(Theme.Motion.standard, value: sending)
    }
}
