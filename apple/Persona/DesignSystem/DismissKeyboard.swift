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
