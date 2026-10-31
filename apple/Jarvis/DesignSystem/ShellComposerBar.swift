import SwiftUI

/// The shell's composer (docs/ios.md「新外壳」), ChatGPT's: one line — [+] 问 Jarvis… [mic] — that grows into a card
/// while typing, the field on top and a row under it: [+] … [mode & effort] [mic] [send]. The field is one view in both
/// shapes (only the buttons around it come and go), so focus and the IME's marked text survive the change.
/// The callers keep their own state (drafts, uploads, SendSafety in `Composer`; the starter's text in StarterComposer).
struct ShellComposerBar<Attach: View, Modes: View>: View {
    var placeholder: String
    @Binding var text: String
    @Binding var selection: TextSelection?
    var focused: FocusState<Bool>.Binding
    var canSend: Bool
    /// Something is attached (photos, quotes): the card stays open, there is something to send.
    var hasAttachments = false
    /// Set while the agent works: a stop button takes send's place while nothing is typed.
    var stop: (() -> Void)?
    /// Mode / effort warns (全部放行): the gauge is red.
    var modesWarn = false
    var send: () -> Void
    @ViewBuilder var attach: () -> Attach
    @ViewBuilder var modes: () -> Modes
    @State private var dictation = Dictation()

    private var expanded: Bool { focused.wrappedValue || !text.isEmpty || hasAttachments }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .bottom, spacing: Theme.Space.xs) {
                if !expanded { plus }
                TextField(placeholder, text: $text, selection: $selection, axis: .vertical)
                    .font(Theme.Font.body)
                    .lineLimit(expanded ? 1...8 : 1...1)
                    .focused(focused)
                    .padding(.horizontal, expanded ? Theme.Space.l : Theme.Space.xs)
                    .padding(.top, expanded ? Theme.Space.l : 0)
                    .scaledFrame(minHeight: Theme.Size.tapTarget)
                    .accessibilityIdentifier("shell-composer-field")
                if !expanded {
                    mic
                    if let stop { stopButton(stop) }
                }
            }
            if expanded {
                HStack(spacing: Theme.Space.xs) {
                    plus
                    Spacer(minLength: Theme.Space.s)
                    Menu { modes() } label: {
                        Image(systemName: Theme.Icon.effort)
                            .font(Theme.Font.heading)
                            .foregroundStyle(modesWarn ? Theme.danger : Color.primary)
                            .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                            .contentShape(.circle)
                    }
                    .menuStyle(.button)
                    .buttonStyle(.plain)
                    .accessibilityLabel("权限和思考强度")
                    .accessibilityIdentifier("shell-composer-modes")
                    mic
                    if canSend {
                        round(Theme.Icon.send, label: "发送", id: "shell-composer-send", action: send)
                            .keyboardShortcut(.return, modifiers: .command)
                    } else if let stop {
                        stopButton(stop)
                    }
                }
                .padding(.horizontal, Theme.Space.s)
                .padding(.bottom, Theme.Space.s)
                .transition(.opacity.combined(with: .move(edge: .bottom)))
            }
        }
        .padding(.horizontal, expanded ? 0 : Theme.Space.xs)
        .padding(.vertical, expanded ? 0 : Theme.Space.xxs)
        .background {
            RoundedRectangle(cornerRadius: Theme.Shell.composerRadius, style: .continuous)
                .fill(Theme.Shell.composer)
                .shadow(color: .black.opacity(0.08), radius: 12, y: 4)
        }
        .overlay {
            RoundedRectangle(cornerRadius: Theme.Shell.composerRadius, style: .continuous)
                .strokeBorder(Theme.Shell.composerStroke, lineWidth: Theme.Size.hairline)
        }
        .contentShape(.rect(cornerRadius: Theme.Shell.composerRadius))
        .onTapGesture { focused.wrappedValue = true }
        .animation(Theme.Motion.layout, value: expanded)
        .animation(Theme.Motion.state, value: canSend)
        .onDisappear { if dictation.isListening { dictation.stop() } }
    }

    private var plus: some View {
        Menu { attach() } label: {
            Image(systemName: Theme.Icon.add)
                .font(Theme.Font.title)
                .fontWeight(.light)
                .foregroundStyle(Color.primary)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.circle)
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .accessibilityLabel("添加照片或文件")
        .accessibilityIdentifier("shell-composer-add")
    }

    /// Speech to text into the field: what is heard goes after what was typed; a second tap ends it.
    private var mic: some View {
        Button(action: toggleDictation) {
            Image(systemName: dictation.isListening ? Theme.Icon.dictating : Theme.Icon.dictate)
                .font(Theme.Font.heading)
                .foregroundStyle(dictation.isListening ? Theme.danger : Color.primary)
                .symbolEffect(.pulse, isActive: dictation.isListening)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(dictation.isListening ? "停止听写" : "听写")
        .accessibilityIdentifier("shell-composer-mic")
    }

    private func stopButton(_ stop: @escaping () -> Void) -> some View {
        round(Theme.Icon.interrupt, label: "中断", id: "shell-composer-stop", action: stop)
    }

    private func round(_ symbol: String, label: String, id: String, action: @escaping () -> Void) -> some View {
        Button {
            if dictation.isListening { dictation.stop() }
            action()
        } label: {
            Image(systemName: symbol)
                .font(Theme.Font.bodyHeavy)
                .foregroundStyle(Theme.Shell.inkText)
                .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                .background(Theme.Shell.ink, in: .circle)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityIdentifier(id)
        .transition(.scale.combined(with: .opacity))
    }

    private func toggleDictation() {
        if dictation.isListening { dictation.stop(); return }
        let base = text.isEmpty || text.hasSuffix(" ") ? text : text + " "
        Task {
            if let problem = await dictation.start(update: { heard in text = base + heard }) {
                AppModel.shared.toast = Toast(problem, style: .error)
            }
        }
    }
}
