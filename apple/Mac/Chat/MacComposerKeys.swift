import AppKit
import SwiftUI

/// ⏎ and ⇧⏎ in a composer (docs/mac.md「细节」: ⏎ 发送、⇧⏎ 换行). The field is a SwiftUI TextField, an AppKit field
/// editor underneath: on its own, ⏎ and ⇧⏎ both ended editing and selected all the text, so the next key typed
/// replaced it, and ⌥⏎ was the only way to a new line (#785).
nonisolated enum MacComposerKeys {
    enum Action: Equatable {
        /// Left to the field, the menus and the send button's ⌘⏎.
        case pass
        case send
        /// A new line at the cursor (what ⌥⏎ does on its own).
        case newline
        /// Taken and dropped: ⏎ with nothing to send, which would otherwise select all the text.
        case ignore
    }

    static let returnKey: UInt16 = 36
    static let keypadEnter: UInt16 = 76

    /// What a key pressed in a focused composer does. While an input method holds text not yet confirmed (pinyin),
    /// ⏎ is its own: it confirms the candidate.
    static func action(keyCode: UInt16, modifiers: NSEvent.ModifierFlags, composing: Bool, canSend: Bool) -> Action {
        guard keyCode == returnKey || keyCode == keypadEnter, !composing else { return .pass }
        switch modifiers.intersection([.command, .shift, .option, .control]) {
        case []: return canSend ? .send : .ignore
        case .shift: return .newline
        default: return .pass
        }
    }
}

/// The wheel over a composer whose text is taller than the field: the field editor follows the cursor but not the
/// wheel, so the wheel moves the clip view around it (AppKit's `_NSKeyboardFocusClipView`).
nonisolated enum MacComposerWheel {
    /// The editor's line height when it has no font to measure.
    static let fallbackLine: CGFloat = 16

    /// The clip view's new top after a wheel step of `delta` points (positive: the wheel turned up, toward the first
    /// line), within `top...bottom`; nil when it cannot move that way, so the page scrolls instead. Flipped
    /// coordinates, as the text view and so its clip view are: a smaller top shows earlier lines.
    static func offset(from y: CGFloat, delta: CGFloat, top: CGFloat, bottom: CGFloat) -> CGFloat? {
        guard bottom > top else { return nil }
        let next = min(max(y - delta, top), bottom)
        return abs(next - y) < 0.5 ? nil : next
    }

    /// Scrolls the field editor's clip view by a wheel event over it; false leaves the event to the page.
    @MainActor static func scroll(_ event: NSEvent, in editor: NSTextView) -> Bool {
        guard let clip = editor.superview as? NSClipView, clip.bounds.contains(clip.convert(event.locationInWindow, from: nil))
        else { return false }
        let line = editor.font.map { editor.layoutManager?.defaultLineHeight(for: $0) ?? $0.pointSize } ?? fallbackLine
        let delta = event.hasPreciseScrollingDeltas ? event.scrollingDeltaY : event.scrollingDeltaY * line
        guard let y = offset(from: clip.bounds.origin.y, delta: delta, top: editor.frame.minY,
                             bottom: editor.frame.maxY - clip.bounds.height) else { return false }
        clip.scroll(to: NSPoint(x: clip.bounds.origin.x, y: y))
        return true
    }
}

extension View {
    /// ⏎ sends and ⇧⏎ starts a new line in a focused composer's field (`MacComposerKeys`); the wheel scrolls a field
    /// grown past its last line (`MacComposerWheel`).
    func macComposerKeys(focused: Bool, canSend: Bool, send: @escaping () -> Void) -> some View {
        modifier(MacComposerEventMonitor(focused: focused, matching: [.keyDown, .scrollWheel]) { event in
            guard let editor = event.window?.firstResponder as? NSTextView else { return event }
            if event.type == .scrollWheel { return MacComposerWheel.scroll(event, in: editor) ? nil : event }
            switch MacComposerKeys.action(keyCode: event.keyCode, modifiers: event.modifierFlags,
                                          composing: editor.hasMarkedText(), canSend: canSend) {
            case .pass: return event
            case .send: send()
            case .newline: editor.insertNewlineIgnoringFieldEditor(nil)
            case .ignore: break
            }
            return nil
        })
    }
}

/// Events of the `matching` kinds in this view's window while its composer has focus, seen before the field gets
/// them: `handle` returns nil to take one. A monitor, because the focused SwiftUI text field (an AppKit field editor)
/// acts on some itself: it takes `paste:` and drops pictures, ⏎ / ⇧⏎ end its editing, the wheel does not scroll it.
/// Only the composer in the window the event is for acts (a conversation can also be open in a window of its own).
struct MacComposerEventMonitor: ViewModifier {
    var focused: Bool
    var matching: NSEvent.EventTypeMask = .keyDown
    var handle: (NSEvent) -> NSEvent?
    @State private var latest = Latest()
    @State private var monitor: Any?

    /// What the monitor reads when an event comes in: the window, the focus and the handler as of the last update,
    /// so it never acts on a stale focus.
    final class Latest {
        weak var window: NSWindow?
        var focused = false
        var handle: (NSEvent) -> NSEvent? = { $0 }
    }

    func body(content: Content) -> some View {
        latest.focused = focused
        latest.handle = handle
        return content
            .background(WindowReader(latest: latest))
            .onAppear { install() }
            .onDisappear {
                if let monitor { NSEvent.removeMonitor(monitor) }
                monitor = nil
            }
    }

    private func install() {
        guard monitor == nil else { return }
        let latest = latest
        monitor = NSEvent.addLocalMonitorForEvents(matching: matching) { event in
            guard latest.focused, let w = latest.window, event.window === w else { return event }
            return latest.handle(event)
        }
    }

    private struct WindowReader: NSViewRepresentable {
        let latest: Latest
        func makeNSView(context: Context) -> NSView { Probe(latest: latest) }
        func updateNSView(_ nsView: NSView, context: Context) {}

        final class Probe: NSView {
            let latest: Latest
            init(latest: Latest) {
                self.latest = latest
                super.init(frame: .zero)
            }
            required init?(coder: NSCoder) { nil }
            override func viewDidMoveToWindow() {
                super.viewDidMoveToWindow()
                latest.window = window
            }
        }
    }
}
