import AppKit

/// 编辑 › 查找… ⌘F: the key window's search field takes the keyboard (Agents, 收件箱, 知识库, 模型… each have a
/// `.searchable`, which AppKit draws as an `NSSearchField` in the toolbar or the page). A window without one is left
/// alone and the item does nothing, as in Finder windows that have no search.
@MainActor enum MacFind {
    /// The menu item: a text view with its own find bar (diffs, output, MonoTextView) keeps ⌘F for that bar.
    static func find() {
        guard let w = NSApp.keyWindow else { return }
        if let tv = w.firstResponder as? NSTextView, tv.usesFindBar {
            let item = NSMenuItem()
            item.tag = NSTextFinder.Action.showFindInterface.rawValue
            tv.performFindPanelAction(item)
            return
        }
        focusSearch(in: w)
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

    private static func visible(_ v: NSView) -> Bool {
        !v.isHiddenOrHasHiddenAncestor && v.window != nil
    }
}
