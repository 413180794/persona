import BeautifulUI
import SwiftUI

// The composer's dictation and stop buttons, the shell's (docs/ios.md「新外壳」): every composer draws these two, so an
// agent's chat and the assistant's look and behave alike (#1079). Each is a size up like the shell's chrome
// (`shellScaled`), wherever it sits.

/// Speech to text into the draft: what is heard goes after what was typed; a second tap ends it. The composer owns the
/// `Dictation` (sending, stopping and leaving the page end it too).
struct ComposerMicButton: View {
    let dictation: Dictation
    @Binding var text: String
    var id = "composer-mic"
    @Environment(\.buiTheme) private var theme

    var body: some View {
        Button(action: toggle) {
            Image(systemName: dictation.isListening ? Theme.Icon.dictating : Theme.Icon.dictate)
                .font(Theme.Font.heading)
                .fontWeight(.medium)
                .foregroundStyle(dictation.isListening ? Theme.danger : ComposerInk.icon(theme))
                .symbolEffect(.pulse, isActive: dictation.isListening)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(dictation.isListening ? "停止听写" : "听写")
        .accessibilityIdentifier(id)
        .shellScaled()
    }

    private func toggle() {
        if dictation.isListening { dictation.stop(); return }
        let base = text.isEmpty || text.last?.isWhitespace == true ? text : text + " "
        Task {
            if let problem = await dictation.start(update: { heard in text = base + heard }) {
                AppModel.shared.toast = Toast(problem, style: .error)
            }
        }
    }
}

/// The composer's round button: a symbol in an ink circle, like ChatGPT's send (the style's accent tints nothing here).
struct ComposerRoundButton: View {
    var symbol: String
    var label: String
    var id: String
    var action: () -> Void
    @Environment(\.buiTheme) private var theme

    var body: some View {
        Button(action: action) {
            Image(systemName: symbol)
                .font(Theme.Font.bodyHeavy)
                .foregroundStyle(AppStyle.current.usesBeautifulUI ? theme.colors.canvas : Theme.Shell.inkText)
                .scaledFrame(width: Theme.Size.wellL, height: Theme.Size.wellL)
                .background(AppStyle.current.usesBeautifulUI ? theme.colors.ink : Theme.Shell.ink, in: .circle)
                .scaledFrame(width: Theme.Size.tapTarget, height: Theme.Size.tapTarget)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityIdentifier(id)
        .shellScaled()
        .transition(.scale.combined(with: .opacity))
    }
}

extension ComposerRoundButton {
    /// Interrupts the agent: shown in send's place while it works and nothing is typed.
    static func stop(id: String = "composer-stop", _ action: @escaping () -> Void) -> ComposerRoundButton {
        ComposerRoundButton(symbol: Theme.Icon.interrupt, label: String(localized: "中断"), id: id, action: action)
    }
}

/// The ink of the composer's icons: the text color, or in the BeautifulUI style its quieter second ink.
enum ComposerInk {
    static func icon(_ theme: BUITheme) -> Color { AppStyle.current.usesBeautifulUI ? theme.colors.ink2 : Color.primary }
}
