import SwiftUI

#if os(iOS)
/// Keeps what you tapped under your finger while it opens or closes. The chat's list is flipped (the newest end is
/// its origin), so a row that grows pushes everything above it up, the tapped header included: a thought jumped
/// 40 pt on opening, and a work line, whose steps load after the tap, went 440 pt up and its header out of reach
/// (2026-10-31, measured frame by frame). Rows report where they are (`chatPinned`); a tap holds that key for a
/// moment (`hold`), and every time layout moves the header the list scrolls it back, until it settles, the time is
/// up or the finger takes the list.
@MainActor final class ChatPin {
    /// The chat list's scroll view (`ChatPin.Finder` in its content). Moved directly: asking `ScrollPosition` for a
    /// point landed on it, then 10 pt off a moment later (the content margin), and back, frame after frame.
    weak var list: UIScrollView?
    /// Set by the chat: forget the position it last asked its list for. At the newest that is the newest end, and the
    /// list went back to it for a frame when a work line's steps arrived.
    var letGo: () -> Void = {}

    /// The coordinate space of the list's content: a place in it does not change when the list scrolls.
    nonisolated static let space = "chatContent"

    /// Where a header is: on screen, and in the list's content. Both come from one layout pass, so the offset that
    /// puts it back is worked out from the content alone; correcting by the screen and the reported offset went past
    /// the mark, and back, when one of the two lagged the other by a frame.
    nonisolated struct Spot: Equatable, Sendable { var y: CGFloat; var content: CGFloat }

    private struct Held { var key: String; var at: Spot; var base: CGFloat; var until: Date; var slow: Bool }
    private var held: Held?
    private var last: [String: Spot] = [:]

    /// Before opening or closing: keep `key` where `from` (the header before, if it changes id) is now. `slow`: what
    /// opens comes later (a work line's steps load from the hub), so the hold lasts its full 1.5 s; otherwise it ends
    /// as soon as the header is back.
    func hold(_ key: String, from: String? = nil, slow: Bool = false) {
        guard let list, let at = last[from ?? key] else { return }
        held = Held(key: key, at: at, base: list.contentOffset.y, until: .now.addingTimeInterval(1.5), slow: slow)
        letGo()
    }

    /// Something is held: following the newest must wait (it scrolled to the end as soon as a work line's steps
    /// arrived, and the header went with it).
    var holding: Bool { held.map { Date.now <= $0.until } ?? false }

    /// A header moved (layout or scrolling).
    func moved(_ key: String, to spot: Spot) {
        last[key] = spot
        guard let h = held, h.key == key, let list else { return }
        if Date.now > h.until || list.isTracking || list.isDecelerating { held = nil; return }
        if !h.slow, abs(spot.y - h.at.y) < 0.5 { held = nil; return }
        // The content runs newest first: rows opening under the header on screen push it further into the content,
        // and the offset has to grow by as much to keep it on screen where it was (as far as the list can go).
        let target = Self.within(list, h.base + spot.content - h.at.content)
        guard abs(target - list.contentOffset.y) >= 0.5 else { return }
        list.contentOffset.y = target
    }

    /// An offset the list can be at.
    private static func within(_ list: UIScrollView, _ y: CGFloat) -> CGFloat {
        let inset = list.adjustedContentInset
        return min(max(y, -inset.top), max(-inset.top, list.contentSize.height + inset.bottom - list.bounds.height))
    }

    /// Sits in the list's content and hands the scroll view around it to the pin.
    struct Finder: UIViewRepresentable {
        let pin: ChatPin

        func makeUIView(context: Context) -> Probe {
            let v = Probe()
            v.pin = pin
            v.isUserInteractionEnabled = false
            return v
        }

        func updateUIView(_ v: Probe, context: Context) { v.pin = pin }

        final class Probe: UIView {
            weak var pin: ChatPin?

            override func didMoveToWindow() {
                super.didMoveToWindow()
                var v = superview
                while let s = v, !(s is UIScrollView) { v = s.superview }
                if let list = v as? UIScrollView { pin?.list = list }
            }
        }
    }
}

#endif

extension EnvironmentValues {
    /// The chat list's pin; nil outside the chat (sheets, other pages), where rows animate as before.
    @Entry var chatPin: ChatPin? = nil
}

extension View {
    /// Reports this row's top to the chat's pin under `key`, so opening or closing it keeps it in place.
    func chatPinned(_ key: String) -> some View { modifier(ChatPinReport(key: key)) }
}

private struct ChatPinReport: ViewModifier {
    let key: String
    @Environment(\.chatPin) private var pin

    func body(content: Content) -> some View {
        if let pin {
            // The content is upside down: the row's top on screen is its far edge there.
            content.onGeometryChange(for: ChatPin.Spot.self, of: {
                ChatPin.Spot(y: $0.frame(in: .global).minY, content: $0.frame(in: .named(ChatPin.space)).maxY)
            }) { pin.moved(key, to: $0) }
        } else {
            content
        }
    }
}

/// Opens or closes something in a row (`change`). In the chat the height changes at once, with the row's top held in
/// place (`ChatPin`): a height animating in the flipped lazy list slid everything above it. What opens animates
/// instead (`unfold`, shown with `.unfolding`): it reveals itself after the change, and on `closing` folds away before
/// it. Elsewhere: the animation as before.
@MainActor func toggleInPlace(_ pin: ChatPin?, _ key: String, animation: Animation, unfold: Binding<Unfolding>? = nil,
                              closing: Bool = false, _ change: @escaping () -> Void) {
    guard let pin else { withAnimation(animation, change); return }
    func now() {
        pin.hold(key)
        var t = Transaction()
        t.disablesAnimations = true
        withTransaction(t, change)
    }
    guard let unfold else { return now() }
    if closing {
        unfold.wrappedValue.concealing = true
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(Theme.Motion.concealTime))
            unfold.wrappedValue.concealing = false
            now()
        }
    } else {
        unfold.wrappedValue.revealing = true
        now()
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(Theme.Motion.revealTime))
            unfold.wrappedValue.revealing = false
        }
    }
}

/// What a row shows when opened, as it comes and goes in the chat (`toggleInPlace`).
struct Unfolding: Equatable {
    /// Just opened: it reveals itself.
    var revealing = false
    /// About to close: it folds away.
    var concealing = false
}

extension View {
    /// What a row shows when opened: it reveals itself from the top (a mask growing down, and a fade) when it appears
    /// while `revealing`, and folds away while `concealing`. Its height never animates, so the list's layout changes
    /// once and the pin holds the header in that same pass.
    func unfolding(_ u: Unfolding) -> some View { modifier(Reveal(revealing: u.revealing, concealing: u.concealing)) }
}

private struct Reveal: ViewModifier {
    var concealing: Bool
    @State private var shown: CGFloat

    init(revealing: Bool, concealing: Bool) {
        self.concealing = concealing
        _shown = State(initialValue: revealing ? 0 : 1)
    }

    func body(content: Content) -> some View {
        content
            .clipShape(RevealShape(shown: shown))
            .opacity(shown)
            .onAppear { if shown < 1 { withAnimation(Theme.Motion.reveal) { shown = 1 } } }
            .onChange(of: concealing) { _, c in withAnimation(c ? Theme.Motion.conceal : Theme.Motion.reveal) { shown = c ? 0 : 1 } }
    }
}

/// The part of the content shown, from its top; fully shown, it clips nothing (shadows and the like reach past it).
nonisolated private struct RevealShape: Shape {
    /// How far past the content a fully shown clip reaches: further than anything draws.
    static let reach: CGFloat = 10_000
    var shown: CGFloat
    var animatableData: CGFloat {
        get { shown }
        set { shown = newValue }
    }

    func path(in rect: CGRect) -> Path {
        guard shown < 1 else { return Path(rect.insetBy(dx: -Self.reach, dy: -Self.reach)) }
        return Path(CGRect(x: rect.minX, y: rect.minY, width: rect.width, height: rect.height * max(shown, 0)))
    }
}
