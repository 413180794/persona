import Foundation

/// A draft grows with the viewport left above the keyboard, bounded to leave room for the conversation.
nonisolated enum ComposerGrowth {
    static func maximumLines(viewportHeight: CGFloat, lineHeight: CGFloat) -> Int {
        guard viewportHeight > 0, lineHeight > 0 else { return Theme.Shell.composerMinimumLines }
        let lines = floor(viewportHeight * Theme.Shell.composerHeightFraction / lineHeight)
        return Int(min(CGFloat(Theme.Shell.composerMaximumLines), max(CGFloat(Theme.Shell.composerMinimumLines), lines)))
    }

    /// Measure at the unreserved width: adding the button must not change the decision that added it.
    static func offersExpansion(textHeight: CGFloat, lineHeight: CGFloat, maximumLines: Int) -> Bool {
        lineHeight > 0 && textHeight > lineHeight * (CGFloat(maximumLines) - Theme.Shell.composerLineTolerance)
    }
}
