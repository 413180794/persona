import AppKit
import SwiftUI
import Testing
@testable import Persona

/// A Mac sheet opens at its content's smallest size. The file viewer's has to be the large sheet's: wrapped in the
/// link panel's GeometryReader it was the toolbar's alone, a 470 × 80 sheet with nothing of the file in it (#1502).
@MainActor struct MacFileViewerSheetTests {
    @Test func theFileViewerAsksForALargeSheet() {
        let viewer = FileViewer(ref: FileRef(machineID: "m", path: "/tmp/a.swift")).environment(AppModel())
        let host = NSHostingView(rootView: viewer)
        // The same window the modifier reads, so a host app's own window can't move the bar.
        let window = NSApp.mainWindow?.contentLayoutRect.size
        let least = MacLargeSheet.fitted(to: window, bars: true)
        let size = host.fittingSize
        #expect(size.width >= least.width, "fitting size \(size)")
        #expect(size.height >= least.height, "fitting size \(size)")
        // Not the whole window: the sheet adds the stack's bars on top, and without room for them it was taller than
        // the main window and trapped in AppKit's layout.
        #expect(size.height < MacLargeSheet.fitted(to: window, bars: false).height, "fitting size \(size)")
    }
}
