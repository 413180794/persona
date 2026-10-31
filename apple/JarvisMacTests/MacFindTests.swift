import AppKit
import Testing
@testable import Jarvis

/// 编辑 › 查找… ⌘F: the window's search field (a page's `.searchable`, which AppKit shows as a toolbar search item or a
/// field in the page) takes the keyboard. The pages are not touched one by one.
@MainActor
struct MacFindTests {
    private func window() -> NSWindow {
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 400, height: 300), styleMask: [.titled], backing: .buffered, defer: false)
        w.isReleasedWhenClosed = false
        return w
    }

    @Test func aFieldInThePageIsFocused() {
        let w = window()
        let field = NSSearchField(frame: NSRect(x: 10, y: 10, width: 200, height: 24))
        let box = NSView(frame: w.contentView!.bounds)
        box.addSubview(field)
        w.contentView!.addSubview(box)
        #expect(MacFind.focusSearch(in: w))
        #expect(MacFind.isEditing(field, in: w))
    }

    @Test func noFieldNoFocus() {
        let w = window()
        w.contentView!.addSubview(NSTextField(frame: NSRect(x: 10, y: 10, width: 200, height: 24)))
        #expect(!MacFind.focusSearch(in: w))
    }

    @Test func hiddenFieldsAreSkipped() {
        let w = window()
        let hidden = NSSearchField(frame: NSRect(x: 10, y: 10, width: 200, height: 24))
        hidden.isHidden = true
        w.contentView!.addSubview(hidden)
        #expect(!MacFind.focusSearch(in: w))
    }
}
