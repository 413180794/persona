import SwiftUI

/// The composer's input line, shared by an agent's chat (`Composer`) and a new assistant conversation
/// (`StarterComposer`) so the two look alike: [+] with the caller's menu, the field (a glass capsule, at least 44 pt),
/// then send (faded while there is nothing to send) or, while the agent works and nothing is typed, a red stop.
/// The callers keep their own state: the chat's drafts, SendSafety and uploads stay in `Composer`.
struct ComposerRow<MenuItems: View>: View {
    var placeholder: String
    @Binding var text: String
    @Binding var selection: TextSelection?
    var focused: FocusState<Bool>.Binding
    var canSend: Bool
    /// Set while the agent works: shown instead of send when there is nothing to send.
    var stop: (() -> Void)?
    var send: () -> Void
    @ViewBuilder var menu: () -> MenuItems

    /// The symbols' frame: the button styles pad it to about 44 pt, the field's height.
    private static var glyph: CGFloat { 31 }

    var body: some View {
        HStack(alignment: .bottom, spacing: 8) {
            Menu { menu() } label: {
                Image(systemName: "plus").font(.body.weight(.semibold)).frame(width: Self.glyph, height: Self.glyph)
            }
            .themedButton()
            .clipShape(.circle)

            TextField(placeholder, text: $text, selection: $selection, axis: .vertical)
                .lineLimit(1...6)
                .focused(focused)
                .padding(.horizontal, 16)
                .padding(.vertical, 11)
                .frame(minHeight: 44)
                .surface(interactive: true, in: .rect(cornerRadius: Theme.Radius.panel, style: .continuous))

            if let stop, !canSend {
                Button(action: stop) {
                    Image(systemName: "stop.fill").font(.body.weight(.bold)).frame(width: Self.glyph, height: Self.glyph)
                }
                .themedButton(prominent: true)
                .tint(Theme.danger)
                .clipShape(.circle)
                .accessibilityLabel("中断")
            } else {
                Button(action: send) {
                    Image(systemName: "arrow.up").font(.body.weight(.bold)).frame(width: Self.glyph, height: Self.glyph)
                }
                .themedButton(prominent: true)
                .clipShape(.circle)
                .disabled(!canSend)
                .keyboardShortcut(.return, modifiers: .command)
                .accessibilityLabel("发送")
            }
        }
    }
}

/// Photos picked for the next message, above the composer: × takes one out; with `edit`, a tap marks it up first.
struct ComposerImages: View {
    @Binding var images: [(UIImage, ImagePayload)]
    var edit: ((Int) -> Void)?

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack {
                ForEach(Array(images.enumerated()), id: \.offset) { idx, pair in
                    Image(uiImage: pair.0).resizable().scaledToFill().frame(width: 56, height: 56)
                        .clipShape(.rect(cornerRadius: Theme.Radius.control, style: .continuous))
                        .overlay(alignment: .bottomLeading) {
                            if edit != nil {
                                Image(systemName: "pencil.tip.crop.circle.fill").font(.caption).foregroundStyle(.white)
                                    .shadow(radius: 2).padding(3)
                            }
                        }
                        .onTapGesture { edit?(idx) }
                        .accessibilityLabel(edit == nil ? "图片 \(idx + 1)" : "图片 \(idx + 1)，点按编辑")
                        .overlay(alignment: .topTrailing) {
                            Button { images.remove(at: idx) } label: {
                                Image(systemName: "xmark.circle.fill").symbolRenderingMode(.palette)
                                    .foregroundStyle(.white, .black.opacity(0.6))
                            }
                            .offset(x: 5, y: -5)
                        }
                }
            }
            .padding(.top, 6)
        }
    }
}
