import AppKit
import SwiftUI
import Testing
@testable import Jarvis

/// Persona's 历史对话 folds the history list away and the conversation takes the window (#856): macOS's three-column
/// split turned `.detailOnly` straight back into `.all`, the empty sidebar column came out and the list stayed.
@MainActor
struct MacListBesideTests {
    static let widthKey = "test.macListBeside.width"

    @Observable final class Widths {
        var list: CGFloat = 0
        var page: CGFloat = 0
        var shown = true
    }

    private struct Probe: View {
        let widths: Widths
        var body: some View {
            MacListBeside(listShown: widths.shown, widthKey: MacListBesideTests.widthKey) {
                Color.clear.onGeometryChange(for: CGFloat.self) { $0.size.width } action: { widths.list = $0 }
            } page: {
                Color.clear.onGeometryChange(for: CGFloat.self) { $0.size.width } action: { widths.page = $0 }
            }
            .frame(width: 1000, height: 600)
        }
    }

    private func settle(_ window: NSWindow) {
        for _ in 0..<5 {
            window.contentView?.layoutSubtreeIfNeeded()
            RunLoop.main.run(until: Date().addingTimeInterval(0.2))
        }
    }

    @Test func foldingTheListGivesThePageTheWholeWidth() {
        UserDefaults.standard.removeObject(forKey: Self.widthKey)
        defer { UserDefaults.standard.removeObject(forKey: Self.widthKey) }
        let widths = Widths()
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1000, height: 600), styleMask: [.titled, .resizable],
                              backing: .buffered, defer: false)
        window.contentView = NSHostingView(rootView: Probe(widths: widths))
        settle(window)
        #expect(widths.list == Theme.Size.sidebar)
        #expect(widths.page < 1000 - Theme.Size.sidebarMin)

        widths.shown = false
        settle(window)
        #expect(widths.page >= 999, "the page takes the window once the list is folded away")

        widths.shown = true
        settle(window)
        #expect(widths.list >= Theme.Size.sidebarMin)
        #expect(widths.page < 1000 - Theme.Size.sidebarMin)
    }

    @Test func theListIsDraggedWithinTheSplitsBounds() {
        #expect(MacListBeside<EmptyView, EmptyView>.clamped(100) == Theme.Size.sidebarMin)
        #expect(MacListBeside<EmptyView, EmptyView>.clamped(420) == 420)
        #expect(MacListBeside<EmptyView, EmptyView>.clamped(900) == Theme.Size.sidebarMax)
    }
}
