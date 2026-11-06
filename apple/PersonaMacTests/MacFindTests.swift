import AppKit
import Testing
@testable import Persona

/// Edit › Find… ⌘F: the window's search field (a page's `.searchable`, which AppKit shows as a toolbar search item or a
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

    // A view with its own find (the code viewer, `MacFindable`) is wired into the same Find… ⌘F.

    @Test func aFindableWithTheKeyboardOpensItsFind() {
        let w = window()
        let field = NSSearchField(frame: NSRect(x: 10, y: 10, width: 200, height: 24))
        let code = FindableView(frame: NSRect(x: 10, y: 40, width: 200, height: 200))
        w.contentView!.addSubview(field)
        w.contentView!.addSubview(code)
        w.makeFirstResponder(code)
        MacFind.find(in: w)
        #expect(code.shown == 1)
        #expect(!MacFind.isEditing(field, in: w))
    }

    @Test func aSearchFieldComesBeforeAFindableWithoutTheKeyboard() {
        let w = window()
        let field = NSSearchField(frame: NSRect(x: 10, y: 10, width: 200, height: 24))
        let code = FindableView(frame: NSRect(x: 10, y: 40, width: 200, height: 200))
        w.contentView!.addSubview(field)
        w.contentView!.addSubview(code)
        MacFind.find(in: w)
        #expect(code.shown == 0)
        #expect(MacFind.isEditing(field, in: w))
    }

    @Test func aWindowWithOnlyAFindableOpensItsFind() {
        // A file open in a sheet: nothing has the keyboard yet, and there is no search field.
        let w = window()
        let hidden = FindableView(frame: NSRect(x: 10, y: 40, width: 200, height: 200))
        hidden.isHidden = true
        let code = FindableView(frame: NSRect(x: 10, y: 40, width: 200, height: 200))
        w.contentView!.addSubview(hidden)
        w.contentView!.addSubview(code)
        MacFind.find(in: w)
        #expect(code.shown == 1)
        #expect(hidden.shown == 0)
    }

    @Test func aTextViewWithAFindBarKeepsIt() {
        let w = window()
        let code = FindableView(frame: NSRect(x: 10, y: 40, width: 200, height: 200))
        let text = NSTextView(frame: NSRect(x: 10, y: 10, width: 200, height: 24))
        text.usesFindBar = true
        w.contentView!.addSubview(code)
        w.contentView!.addSubview(text)
        w.makeFirstResponder(text)
        MacFind.find(in: w)
        #expect(code.shown == 0)
        MacFind.findNext(in: w, forward: true)
        #expect(code.steps.isEmpty)
    }

    // Find Next ⌘G / Find Previous ⇧⌘G go to the same view Find… would open.

    @Test func findNextGoesToTheFindableWithTheKeyboard() {
        let w = window()
        let field = NSSearchField(frame: NSRect(x: 10, y: 10, width: 200, height: 24))
        let code = FindableView(frame: NSRect(x: 10, y: 40, width: 200, height: 200))
        w.contentView!.addSubview(field)
        w.contentView!.addSubview(code)
        w.makeFirstResponder(code)
        MacFind.findNext(in: w, forward: true)
        MacFind.findNext(in: w, forward: false)
        #expect(code.steps == [true, false])
    }

    @Test func findNextReachesAVisibleFindableWithoutTheKeyboard() {
        // The find field has the keyboard (not in the code view's responder chain), or nothing was clicked yet.
        let w = window()
        let hidden = FindableView(frame: NSRect(x: 10, y: 40, width: 200, height: 200))
        hidden.isHidden = true
        let code = FindableView(frame: NSRect(x: 10, y: 40, width: 200, height: 200))
        w.contentView!.addSubview(hidden)
        w.contentView!.addSubview(code)
        MacFind.findNext(in: w, forward: false)
        #expect(code.steps == [false])
        #expect(hidden.steps.isEmpty)
    }
}

private final class FindableView: NSView, MacFindable {
    var shown = 0
    var steps: [Bool] = []
    override var acceptsFirstResponder: Bool { true }
    func showFind() { shown += 1 }
    func findNext(forward: Bool) { steps.append(forward) }
}
