import SwiftUI
import UIKit

enum Keyboard {
    /// Closes the keyboard of whatever field has it (the terminal's own input included: callers decide where this runs).
    static func dismiss() {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
    }
}

extension View {
    /// A tap on the page, anywhere the content did not take it, closes the keyboard. Runs next to the content's own
    /// gestures (`simultaneousGesture`), so rows and buttons still get their taps.
    func dismissesKeyboardOnTap() -> some View {
        simultaneousGesture(TapGesture().onEnded { Keyboard.dismiss() })
    }
}
