#if os(macOS)
import AppKit
import SwiftUI

// The Mac's versions of the UIKit gestures and text view in MessageMenu.swift: same names, same call sites.

/// The phases `MessageMenu.pressChanged` follows (UIGestureRecognizer.State's, which AppKit's states map onto).
enum PressPhase: Sendable {
    case possible, began, changed, ended, cancelled, failed

    init(_ state: NSGestureRecognizer.State) {
        switch state {
        case .began: self = .began
        case .changed: self = .changed
        case .ended: self = .ended
        case .cancelled: self = .cancelled
        case .failed: self = .failed
        default: self = .possible
        }
    }
}

/// iOS swipes between the chat's panes with a sideways flick. A Mac has no such gesture on a view (a trackpad
/// swipe arrives as scroll events, which gesture recognizers never see), so this is a recognizer that never fires;
/// the panes are switched with their buttons.
struct SwipeTo: NSGestureRecognizerRepresentable {
    struct Direction: OptionSet, Sendable {
        let rawValue: Int
        static let right = Direction(rawValue: 1 << 0)
        static let left = Direction(rawValue: 1 << 1)
        static let up = Direction(rawValue: 1 << 2)
        static let down = Direction(rawValue: 1 << 3)
    }

    var direction: Direction
    var action: () -> Void

    func makeNSGestureRecognizer(context: Context) -> NSGestureRecognizer {
        let g = NSGestureRecognizer()
        g.isEnabled = false
        return g
    }

    func handleNSGestureRecognizerAction(_ recognizer: NSGestureRecognizer, context: Context) {}
}

/// A long press on iOS is a secondary click on a Mac (right click, two-finger click, control-click): it reports
/// where the pointer is (in the enclosing `messageActionBar` row's coordinates, `MessageMenu.row`) and the press's
/// phases, so the message action bar opens at the click.
struct LongPressAt: NSGestureRecognizerRepresentable {
    var changed: (PressPhase) -> Void = { _ in }
    var action: (CGPoint) -> Void

    func makeNSGestureRecognizer(context: Context) -> SecondaryClickRecognizer { SecondaryClickRecognizer() }

    func handleNSGestureRecognizerAction(_ recognizer: SecondaryClickRecognizer, context: Context) {
        changed(PressPhase(recognizer.state))
        if recognizer.state == .began { action(context.converter.location(in: .named(MessageMenu.row))) }
    }
}

/// Begins on a right mouse down (or a control-click) and ends when that button lifts; plain clicks fail at once and
/// are never delayed, so buttons, links and text selection underneath work as before.
final class SecondaryClickRecognizer: NSGestureRecognizer {
    override init(target: Any?, action: Selector?) {
        super.init(target: target, action: action)
        delaysPrimaryMouseButtonEvents = false
        delaysSecondaryMouseButtonEvents = false
        delaysOtherMouseButtonEvents = false
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

    /// Where the press went down, in window coordinates. AppKit tracks the location only for its own recognizers: one
    /// that handles the events itself reports (0, 0), the window's bottom-left corner, which put the action bar there
    /// (#501). SwiftUI's coordinate converter reads it through `location(in:)`.
    private var downAt: NSPoint?

    override func location(in view: NSView?) -> NSPoint {
        guard let downAt else { return super.location(in: view) }
        return view?.convert(downAt, from: nil) ?? downAt
    }

    override func rightMouseDown(with event: NSEvent) {
        downAt = event.locationInWindow
        state = .began
    }
    override func rightMouseUp(with event: NSEvent) { finish() }
    override func mouseDown(with event: NSEvent) {
        downAt = event.locationInWindow
        state = event.modifierFlags.contains(.control) ? .began : .failed
    }
    override func mouseUp(with event: NSEvent) { finish() }

    override func reset() {
        downAt = nil
        super.reset()
    }

    private func finish() {
        if state == .began || state == .changed { state = .ended } else { state = .failed }
    }
}

/// `SelectableTextView` ("Select Text"): the message in a read-only, selectable text view.
struct SelectableTextView: NSViewRepresentable {
    var text: String
    /// Your own message; the Mac shows every message as written.
    var plain = false
    /// Quote for the selection, first in its right-click menu (none when nil).
    var quote: ((String) -> Void)? = nil

    func makeNSView(context: Context) -> NSScrollView {
        let scroll = MenuTextView.scrollable()
        let coordinator = context.coordinator
        (scroll.documentView as? MenuTextView)?.selectionItems = { selected in
            guard coordinator.quote != nil else { return [] }
            return [UIAction(title: String(localized: "Quote"), image: UIImage(systemName: Theme.Icon.quote)) { _ in
                coordinator.quote?(selected)
            }.nsMenuItem()]
        }
        return scroll
    }

    func updateNSView(_ v: NSScrollView, context: Context) {
        context.coordinator.quote = quote
        guard let t = v.documentView as? NSTextView else { return }
        if t.string != text { t.string = text }
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    @MainActor
    final class Coordinator {
        var quote: ((String) -> Void)?
    }
}

/// A read-only, selectable NSTextView whose right-click menu can lead with the caller's items for the selection.
final class MenuTextView: NSTextView {
    /// Items for the selected text (not called when nothing is selected).
    var selectionItems: ((String) -> [NSMenuItem])?

    /// In a scroll view, set up like the iOS text views: body font, page insets, clear background.
    static func scrollable() -> NSScrollView {
        let scroll = NSScrollView()
        scroll.hasVerticalScroller = true
        scroll.autohidesScrollers = true
        scroll.drawsBackground = false
        let t = MenuTextView()
        t.isEditable = false
        t.isSelectable = true
        t.isRichText = false
        t.drawsBackground = false
        t.font = .preferredFont(forTextStyle: .body)
        t.textColor = .labelColor
        t.textContainerInset = NSSize(width: Theme.Space.m, height: Theme.Space.l)
        t.isVerticallyResizable = true
        t.isHorizontallyResizable = false
        t.autoresizingMask = [.width]
        t.textContainer?.widthTracksTextView = true
        scroll.documentView = t
        return scroll
    }

    override func menu(for event: NSEvent) -> NSMenu? {
        let menu = super.menu(for: event) ?? NSMenu()
        let range = selectedRange()
        guard let selectionItems, range.length > 0 else { return menu }
        let selected = (string as NSString).substring(with: range).trimmingCharacters(in: .whitespacesAndNewlines)
        guard !selected.isEmpty else { return menu }
        let ours = selectionItems(selected)
        guard !ours.isEmpty else { return menu }
        menu.insertItem(.separator(), at: 0)
        for item in ours.reversed() { menu.insertItem(item, at: 0) }
        return menu
    }
}
#endif
