import AppKit
import SwiftUI

/// The Mac's `ChatPin` (iOS: `Views/Chat/ChatPin.swift`): keeps a clicked header where it was while what it opens
/// settles. Same calls as on iOS; the chat list is an `NSScrollView` here, moved through its clip view.
@MainActor final class ChatPin {
    weak var list: NSScrollView?
    var letGo: () -> Void = {}

    nonisolated static let space = "chatContent"
    /// How long a tapped header is held at most.
    nonisolated static let holdTime: Double = 1.5

    nonisolated struct Spot: Equatable, Sendable { var y: CGFloat; var content: CGFloat }

    private struct Held { var key: String; var at: Spot; var base: CGFloat; var until: Date; var slow: Bool; var rise: CGFloat }
    private var held: Held?
    private var last: [String: Spot] = [:]

    /// `rise`: how far up the header may go to show what opens under it (as on iOS).
    /// `keep`: held until `release()` (as on iOS).
    func hold(_ key: String, from: String? = nil, slow: Bool = false, rise: CGFloat = 0, keep: Bool = false) {
        guard let list, let at = last[from ?? key] else { return }
        // A row under the kept header: the kept hold has it in place already (as on iOS).
        if !keep, let k = held, k.until == .distantFuture, let kept = last[k.key], at.content < kept.content { return }
        held = Held(key: key, at: at, base: Self.offset(list), until: keep ? .distantFuture : .now.addingTimeInterval(Self.holdTime),
                    slow: slow || keep, rise: max(rise, 0))
        letGo()
    }

    var holding: Bool { held.map { Date.now <= $0.until } ?? false }

    func release() { held = nil }

    /// The kept hold lasts the usual time from now (as on iOS).
    func unkeep() {
        if var h = held, h.until == .distantFuture {
            h.until = .now.addingTimeInterval(Self.holdTime)
            held = h
        }
    }

    func moved(_ key: String, to spot: Spot) {
        last[key] = spot
        guard let h = held, h.key == key, let list else { return }
        if Date.now > h.until { held = nil; return }
        if !h.slow, abs(spot.y - h.at.y) < 0.5 { held = nil; return }
        let grown = spot.content - h.at.content
        let target = Self.within(list, h.base + (grown > 0 ? max(grown - h.rise, 0) : grown))
        guard abs(target - Self.offset(list)) >= 0.5 else { return }
        list.contentView.scroll(to: NSPoint(x: list.contentView.bounds.origin.x, y: target))
        list.reflectScrolledClipView(list.contentView)
    }

    /// iOS brings what opened at the newest end above the composer; a Mac window is tall enough that it stays in view.
    func show(_ key: String, bottom: CGFloat) {}

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
