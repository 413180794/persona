import AppKit
import SwiftUI

/// The keys of Agents' and Persona's lists, as 收件箱's (#2113): ⏎ and Tab go to the open conversation's composer, Delete
/// asks to delete (Agents) or archives (Persona) it; ⇧Tab in a composer comes back (`MacShellState.focusList`). ↑ / ↓
/// are a selecting list's own (`MacSelectingList`, Persona's).
struct MacListKeys: ViewModifier {
    /// The open conversation: what the keys act on.
    var current: () -> SessionKey?
    var delete: ((SessionKey) -> Void)?
    @FocusState private var isFocused: Bool

    func body(content: Content) -> some View {
        content
            .focused($isFocused)
            .onKeyPress(.return) { toComposer() }
            .onKeyPress(.tab) { toComposer() }
            .onDeleteCommand { if let key = current() { delete?(key) } }
            .onChange(of: MacShellState.shared.focusList) { isFocused = true }
    }

    private func toComposer() -> KeyPress.Result {
        guard let key = current() else { return .ignored }
        AppModel.shared.composerFocus = key
        return .handled
    }
}

extension MacShellState {
    /// ⇧Tab in a composer: the list beside it takes the keys again (`MacListKeys`).
    func focusListAgain() { focusList += 1 }
}

#if DEBUG
/// `-showMacKeyScript "6:shift+tab 7:down 8:return"` (DEBUG): at each second after launch, that key to the main
/// window, as if typed; `-openMacListFocus <seconds>`: the list takes the keys then. For checking the lists' keys while
/// the Mac is locked (`scripts/app/mac_shot.sh`): events posted to the system do not reach the window then.
enum MacKeyScript {
    static func start() {
        let d = UserDefaults.standard
        if d.object(forKey: "openMacListFocus") != nil {
            let at = d.double(forKey: "openMacListFocus")
            DispatchQueue.main.asyncAfter(deadline: .now() + at) { MacShellState.shared.focusListAgain() }
        }
        guard let script = d.string(forKey: "showMacKeyScript") else { return }
        for step in script.split(separator: " ") {
            let parts = step.split(separator: ":", maxSplits: 1)
            guard parts.count == 2, let at = Double(parts[0]) else { continue }
            let name = String(parts[1])
            DispatchQueue.main.asyncAfter(deadline: .now() + at) { send(name) }
        }
    }

    private static let codes: [String: (UInt16, String)] = [
        "down": (125, String(UnicodeScalar(NSDownArrowFunctionKey)!)), "up": (126, String(UnicodeScalar(NSUpArrowFunctionKey)!)),
        "return": (36, "\r"), "tab": (48, "\t"), "delete": (51, "\u{7f}"),
    ]

    private static func send(_ name: String) {
        let shift = name.hasPrefix("shift+")
        guard let (code, chars) = codes[shift ? String(name.dropFirst(6)) : name],
              let window = NSApp.windows.first(where: { $0.identifier?.rawValue.hasPrefix("main") == true }) else { return }
        for type in [NSEvent.EventType.keyDown, .keyUp] {
            if let e = NSEvent.keyEvent(with: type, location: .zero, modifierFlags: shift ? .shift : [], timestamp: ProcessInfo.processInfo.systemUptime,
                                        windowNumber: window.windowNumber, context: nil, characters: chars,
                                        charactersIgnoringModifiers: chars, isARepeat: false, keyCode: code) {
                window.sendEvent(e)
            }
        }
    }
}
#endif
