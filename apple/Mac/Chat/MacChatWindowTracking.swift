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
        // A section's page kept behind another one (`MacPageStack`): its chat is not the one the menus act on.
        .background {
            PageFrontWatcher(initial: false) { front in
                guard let window else { return }
                let shell = MacShellState.shared
                if front { shell.chatsByWindow[window] = key } else if shell.chatsByWindow[window] == key { shell.chatsByWindow[window] = nil }
            }
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
        private var keyObserver: NSObjectProtocol?

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if let keyObserver { NotificationCenter.default.removeObserver(keyObserver) }
            keyObserver = nil
            // Read on the next turn: a window not yet on screen can still report -1 here.
            DispatchQueue.main.async { [weak self] in self?.report() }
            // And again whenever it becomes key, in case that first read came too early.
            if let window {
                keyObserver = NotificationCenter.default.addObserver(forName: NSWindow.didBecomeKeyNotification, object: window,
                                                                     queue: .main) { [weak self] _ in
                    MainActor.assumeIsolated { self?.report() }
                }
            }
        }

        // The observer goes when the view leaves its window (above, with a nil window); it holds the probe weakly.
        private func report() { changed(window.map(\.windowNumber).flatMap { $0 > 0 ? $0 : nil }) }
    }
}
