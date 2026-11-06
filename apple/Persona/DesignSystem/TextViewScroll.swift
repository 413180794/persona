import SwiftUI

#if os(iOS) || os(visionOS)
import UIKit

/// Brings a growing text field's text back into view (#909). A vertical `TextField` is a text view that scrolls to
/// keep the caret in sight. When a new line arrives before SwiftUI has made the field taller, it scrolls down a line;
/// when the field then grows to fit, the offset can stay where it was. On a phone with a third-party keyboard the
/// composer ended up two lines tall showing only the second line, with an empty line under it and the first line
/// scrolled out of sight. Nothing reads wrong while the offset is within the content, so only an offset past the end
/// is moved.
enum TextViewScroll {
    /// The offset to keep: the view's own, unless it is past the last position its content allows.
    nonisolated static func settled(offset: CGFloat, content: CGFloat, height: CGFloat, insetTop: CGFloat, insetBottom: CGFloat) -> CGFloat {
        let top = -insetTop
        let end = max(top, content + insetBottom - height)
        return min(offset, end)
    }

    /// Settles `view` (see `settled`).
    @MainActor static func settle(_ view: UIScrollView) {
        let inset = view.adjustedContentInset
        let y = settled(offset: view.contentOffset.y, content: view.contentSize.height, height: view.bounds.height,
                        insetTop: inset.top, insetBottom: inset.bottom)
        guard y < view.contentOffset.y - 0.5 else { return }
        view.setContentOffset(CGPoint(x: view.contentOffset.x, y: y), animated: false)
    }

    /// Settles the text view that has focus, once the layout in progress is done.
    @MainActor static func settleFocused(revealCaret: Bool = false) {
        DispatchQueue.main.async {
            if let view = FirstResponder.current as? UITextView {
                if revealCaret { view.layoutIfNeeded() }
                settle(view)
                // Reserving the corner control changes wrapping while the field grows. Keep the newly typed line
                // visible after that layout, without changing its text or selection.
                if revealCaret, view.selectedRange.location != NSNotFound {
                    view.scrollRangeToVisible(view.selectedRange)
                }
            }
        }
    }
}

/// Whatever UIKit says has focus: an action sent to no target goes to the first responder, which records itself.
enum FirstResponder {
    nonisolated(unsafe) fileprivate static weak var found: UIResponder?

    @MainActor static var current: UIResponder? {
        found = nil
        UIApplication.shared.sendAction(#selector(UIResponder.personaRecordFirstResponder(_:)), to: nil, from: nil, for: nil)
        defer { found = nil }
        return found
    }
}

extension UIResponder {
    @objc fileprivate func personaRecordFirstResponder(_ sender: Any?) { FirstResponder.found = self }
}
#endif
