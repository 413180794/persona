import AppKit

/// Edit › Find… ⌘F: the key window's search field takes the keyboard (Agents, Inbox, Knowledge, Model… each have a
/// `.searchable`, which AppKit draws as an `NSSearchField` in the toolbar or the page). A window without one is left
/// alone and the item does nothing, as in Finder windows that have no search. A view with its own find (`MacFindable`:
/// the code viewer) opens it instead when it has the keyboard, or when the window has no search field.
@MainActor enum MacFind {
    /// The menu item: a text view with its own find bar (diffs, output, MonoTextView) keeps ⌘F for that bar.
    static func find() {
        guard let w = NSApp.keyWindow else { return }
        find(in: w)
    }

    static func find(in w: NSWindow) {
        if let tv = w.firstResponder as? NSTextView, tv.usesFindBar {
            let item = NSMenuItem()
            item.tag = NSTextFinder.Action.showFindInterface.rawValue
            tv.performFindPanelAction(item)
            return
        }
        // A view with its own find (the code viewer) that has the keyboard, or is in its responder chain.
        var r = w.firstResponder
        while let x = r {
            if let f = x as? MacFindable {
                f.showFind()
                return
            }
            r = x.nextResponder
        }
        if focusSearch(in: w) { return }
        // No search field: a file open in a sheet, nothing clicked yet.
        let roots = [w.contentView?.superview ?? w.contentView].compactMap { $0 }
        if let f = roots.flatMap(findables).first(where: { visible($0) }) { (f as? MacFindable)?.showFind() }
    }

    @discardableResult static func focusSearch(in window: NSWindow) -> Bool {
        // The toolbar's search item first (`.searchable(placement: .toolbar)`), then a field in the page.
        let toolbar = window.toolbar?.items.compactMap { ($0 as? NSSearchToolbarItem)?.searchField } ?? []
        let roots = [window.contentView?.superview ?? window.contentView].compactMap { $0 }
        guard let field = (toolbar + roots.flatMap(searchFields)).first(where: visible) else { return false }
        return window.makeFirstResponder(field)
    }

    /// The field has the keyboard (its field editor is the window's first responder).
    static func isEditing(_ field: NSTextField, in window: NSWindow) -> Bool {
        guard let editor = window.firstResponder as? NSText else { return window.firstResponder === field }
        return editor.delegate === field
    }

    private static func searchFields(_ v: NSView) -> [NSSearchField] {
        (v as? NSSearchField).map { [$0] } ?? v.subviews.flatMap(searchFields)
    }

    private static func findables(_ v: NSView) -> [NSView] {
        (v is MacFindable ? [v] : []) + v.subviews.flatMap(findables)
    }

    private static func visible(_ v: NSView) -> Bool {
        !v.isHiddenOrHasHiddenAncestor && v.window != nil
    }
}

/// A view with its own find (the code viewer's find bar): Find… ⌘F opens it.
@MainActor protocol MacFindable: AnyObject {
    func showFind()
    func findNext(forward: Bool)
}

extension MacFind {
    /// Edit › Find Next ⌘G / Find Previous ⇧⌘G: to whatever Find… opened. A text view with its own find bar (diffs, output)
    /// steps through its matches; the code viewer (`MacFindable`) with the keyboard or in the window steps through
    /// its own. A window with only a search field has nothing to step through.
    static func findNext(forward: Bool) {
        guard let w = NSApp.keyWindow else { return }
        findNext(in: w, forward: forward)
    }

    static func findNext(in w: NSWindow, forward: Bool) {
        if let tv = w.firstResponder as? NSTextView, tv.usesFindBar {
            // The find field's own editor is a text view too, but not one with a find bar: it falls through to the
            // code viewer below.
            let item = NSMenuItem()
            item.tag = (forward ? NSTextFinder.Action.nextMatch : .previousMatch).rawValue
            tv.performFindPanelAction(item)
            return
        }
        var r = w.firstResponder
        while let x = r {
            if let f = x as? MacFindable {
                f.findNext(forward: forward)
                return
            }
            r = x.nextResponder
        }
        let roots = [w.contentView?.superview ?? w.contentView].compactMap { $0 }
        if let f = roots.flatMap(findables).first(where: { visible($0) }) { (f as? MacFindable)?.findNext(forward: forward) }
    }
}
