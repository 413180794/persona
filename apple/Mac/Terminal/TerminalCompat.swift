#if os(macOS)
import Foundation

/// `UISwipeGestureRecognizer.Direction` for the terminal's `onSwipe`: the shared session page switches agents on it,
/// and on a Mac the swipe comes from the trackpad (`NSResponder.swipe(with:)`).
enum UISwipeGestureRecognizer {
    struct Direction: OptionSet, Hashable, Sendable {
        let rawValue: UInt
        static let right = Direction(rawValue: 1 << 0)
        static let left = Direction(rawValue: 1 << 1)
        static let up = Direction(rawValue: 1 << 2)
        static let down = Direction(rawValue: 1 << 3)
    }
}
#endif
