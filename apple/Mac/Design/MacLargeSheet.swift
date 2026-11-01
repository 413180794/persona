#if os(macOS)
import AppKit
import SwiftUI

extension View {
    /// A sheet that stands in for something iOS shows over the whole screen (the terminal cover, an image) or a
    /// reader that wants room (a file, a diff): it opens as large as the window it comes from, less a margin,
    /// never smaller than a form sheet (`macSheet`). Sized once, when it opens.
    /// `bars`: this is a navigation stack's content, whose title bar and bottom toolbar come on top of its height.
    func macLargeSheet(bars: Bool = false) -> some View { modifier(MacLargeSheet(bars: bars)) }
}

struct MacLargeSheet: ViewModifier {
    var bars: Bool
    @State private var window = NSApp.mainWindow?.contentLayoutRect.size

    func body(content: Content) -> some View {
        // A minimum: a sheet opens at its content's minimum size (ideal sizes are ignored). A fixed frame on a
        // navigation stack left the stack short under an empty band; on a stack's content it is fine either way.
        let size = Self.fitted(to: window, bars: bars)
        return content.frame(minWidth: size.width, minHeight: size.height)
    }

    /// The window's content less the margin on each side (and the bars); the main window's first size when there is none.
    static func fitted(to window: CGSize?, bars: Bool) -> CGSize {
        let w = window ?? CGSize(width: Theme.Size.macWindowWidth, height: Theme.Size.macWindowHeight)
        let margin = Theme.Space.xxl * 2
        return CGSize(width: max(Theme.Size.macSheetMinWidth, w.width - margin),
                      height: max(Theme.Size.macSheetMinHeight, w.height - margin - (bars ? Theme.Size.macSheetBars : 0)))
    }
}
#endif
