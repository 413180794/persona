import SwiftUI
#if canImport(UIKit)
import UIKit
#endif

enum Keyboard {
    /// Closes the keyboard of whatever field has it (the terminal's own input included: callers decide where this runs).
    static func dismiss() {
        #if canImport(UIKit)
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
        #endif
    }
}

/// Whether the on-screen keyboard is up (a hardware keyboard's bar of shortcuts does not count). Always false where
/// there is none (the Mac). The pending cards take the conversation's room while it is up (#1912).
@Observable final class KeyboardState {
    static let shared = KeyboardState()
    private(set) var isUp = false
    /// Taller than a hardware keyboard's bar, shorter than any on-screen keyboard.
    private static let least: CGFloat = 120

    private init() {
        #if os(iOS)
        let center = NotificationCenter.default
        center.addObserver(forName: UIResponder.keyboardWillShowNotification, object: nil, queue: .main) { @Sendable note in
            let height = (note.userInfo?[UIResponder.keyboardFrameEndUserInfoKey] as? CGRect)?.height ?? 0
            MainActor.assumeIsolated { KeyboardState.shared.isUp = height > KeyboardState.least }
        }
        center.addObserver(forName: UIResponder.keyboardWillHideNotification, object: nil, queue: .main) { @Sendable _ in
            MainActor.assumeIsolated { KeyboardState.shared.isUp = false }
        }
        #endif
    }
}

extension View {
    /// A tap on the page, anywhere the content did not take it, closes the keyboard. Runs next to the content's own
    /// gestures (`simultaneousGesture`), so rows and buttons still get their taps.
    func dismissesKeyboardOnTap() -> some View {
        #if canImport(UIKit)
        simultaneousGesture(TapGesture().onEnded { Keyboard.dismiss() })
        #else
        self
        #endif
    }
}
