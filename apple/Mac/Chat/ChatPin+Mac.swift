import AppKit
import SwiftUI

/// The Mac's `ChatPin` (iOS: `Views/Chat/ChatPin.swift`): keeps a clicked header where it was while what it opens
/// settles. Same calls as on iOS; the chat list is an `NSScrollView` here, moved through its clip view.
@MainActor final class ChatPin {
    weak var list: NSScrollView?
    var letGo: () -> Void = {}

    nonisolated static let space = "chatContent"

    nonisolated struct Spot: Equatable, Sendable { var y: CGFloat; var content: CGFloat }

    private struct Held { var key: String; var at: Spot; var base: CGFloat; var until: Date; var slow: Bool }
    private var held: Held?
    private var last: [String: Spot] = [:]

    func hold(_ key: String, from: String? = nil, slow: Bool = false) {
        guard let list, let at = last[from ?? key] else { return }
        held = Held(key: key, at: at, base: Self.offset(list), until: .now.addingTimeInterval(1.5), slow: slow)
        letGo()
    }

    var holding: Bool { held.map { Date.now <= $0.until } ?? false }

    func moved(_ key: String, to spot: Spot) {
        last[key] = spot
        guard let h = held, h.key == key, let list else { return }
        if Date.now > h.until { held = nil; return }
        if !h.slow, abs(spot.y - h.at.y) < 0.5 { held = nil; return }
        let target = Self.within(list, h.base + spot.content - h.at.content)
        guard abs(target - Self.offset(list)) >= 0.5 else { return }
        list.contentView.scroll(to: NSPoint(x: list.contentView.bounds.origin.x, y: target))
        list.reflectScrolledClipView(list.contentView)
    }

    private static func offset(_ list: NSScrollView) -> CGFloat { list.contentView.bounds.origin.y }

    private static func within(_ list: NSScrollView, _ y: CGFloat) -> CGFloat {
        let inset = list.contentInsets
        let height = list.documentView?.frame.height ?? 0
        return min(max(y, -inset.top), max(-inset.top, height + inset.bottom - list.contentView.bounds.height))
    }

    /// Sits in the list's content and hands the scroll view around it to the pin.
    struct Finder: NSViewRepresentable {
        let pin: ChatPin

        func makeNSView(context: Context) -> Probe {
            let v = Probe()
            v.pin = pin
            return v
        }

        func updateNSView(_ v: Probe, context: Context) { v.pin = pin }

        final class Probe: NSView {
            weak var pin: ChatPin?

            override func hitTest(_ point: NSPoint) -> NSView? { nil }

            override func viewDidMoveToWindow() {
                super.viewDidMoveToWindow()
                if let list = enclosingScrollView { pin?.list = list }
            }
        }
    }
}
