import AppKit
import SwiftUI

extension View {
    /// The window this view is in, by number (nil until it is in one). The Agent menu's requests carry the key
    /// window's number, and a chat answers only those for its own window.
    func macWindowNumber(_ number: Binding<Int?>) -> some View {
        background(MacWindowNumberReader(number: number))
    }

    /// A chat registers what it shows under its window (`MacShellState.chatsByWindow`) while on screen, so the menus
    /// act on the key window's chat.
    func macRegistersChat(_ key: SessionKey, window: Int?) -> some View {
        onChange(of: window, initial: true) { old, new in
            let shell = MacShellState.shared
            if let old, shell.chatsByWindow[old] == key { shell.chatsByWindow[old] = nil }
            if let new { shell.chatsByWindow[new] = key }
        }
        .onDisappear {
            if let window, MacShellState.shared.chatsByWindow[window] == key { MacShellState.shared.chatsByWindow[window] = nil }
        }
    }
}

private struct MacWindowNumberReader: NSViewRepresentable {
    @Binding var number: Int?

    func makeNSView(context: Context) -> Probe {
        let p = Probe()
        p.changed = { n in if number != n { number = n } }
        return p
    }

    func updateNSView(_ p: Probe, context: Context) {
        p.changed = { n in if number != n { number = n } }
    }

    final class Probe: NSView {
        var changed: (Int?) -> Void = { _ in }
        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            let n = window?.windowNumber
            DispatchQueue.main.async { [weak self] in self?.changed(n) }
        }
    }
}
