import SwiftUI

/// Where a Feishu chat's (or thread's) list stands against its newest end, worked out from its scroll geometry.
/// The end is an exact offset: the content's height less the part of the list between its insets (the top bar above,
/// the box, the quote bar and the keyboard below).
nonisolated enum LarkFollow {
    struct Geometry: Equatable {
        /// `ScrollGeometry.contentOffset.y`: the inset top is `-top`.
        var offset: CGFloat
        var content: CGFloat
        /// The list's height between its insets.
        var container: CGFloat
        var top: CGFloat
        var bottom: CGFloat
    }

    /// This close to the end is at the end (fractional layout).
    static let slack: CGFloat = 1

    /// The offset that shows the end of the content right above the bottom inset (the top, when it all fits).
    static func endOffset(_ g: Geometry) -> CGFloat {
        max(-g.top, g.content - g.container - g.top)
    }

    static func atEnd(_ g: Geometry) -> Bool {
        abs(endOffset(g) - g.offset) <= slack
    }

    /// Whether the list should be sent to its end after its geometry changed from `old` to `new`.
    /// - Following: whenever it is not there (the size-change anchor keeps it there; this catches the rest).
    /// - Not following: only a list that showed its end before the keyboard, box or quote bar took room keeps
    ///   showing it (a short thread under the keyboard); a list read further up stays where it is.
    static func goesToEnd(old: Geometry?, new: Geometry, following: Bool) -> Bool {
        if atEnd(new) { return false }
        if following { return true }
        guard let old, atEnd(old), old.content == new.content else { return false }
        return old.bottom != new.bottom || old.container != new.container
    }
}

extension View {
    /// Keeps a chat's list at its newest end while it follows (LarkFollow): the keyboard coming up, the box growing,
    /// the quote bar, a message sent or arriving all move the end, and the list goes with it. While following, the
    /// scroll view's own bottom anchor takes size changes (it knows the lazy rows' real heights; an offset worked out
    /// from their estimates, or an id-anchored scroll to a row just added, lands short), and a jump to the end
    /// (`scrollTo(edge:)`, not animated: an animated scroll started while the keyboard moves the insets is dropped)
    /// catches what is left. The user scrolling stops following; a scroll that ends at the end starts it again, like
    /// the Agents chat's followsEnd (ChatView). `resting` is the size-change anchor while not following (nil: the
    /// scroll view's default). `active` is off while the page lands where it opens (a first unread message): nothing
    /// moves it until then.
    func larkFollowsEnd(_ position: Binding<ScrollPosition>, following: Binding<Bool>, resting: UnitPoint? = nil,
                        active: Bool) -> some View {
        modifier(LarkFollowsEnd(position: position, following: following, resting: resting, active: active))
    }
}

private struct LarkFollowsEnd: ViewModifier {
    @Binding var position: ScrollPosition
    @Binding var following: Bool
    var resting: UnitPoint?
    var active: Bool
    @State private var geometry: LarkFollow.Geometry?
    @State private var phase: ScrollPhase = .idle
    /// The end the last jump went for and when: a jump lands a moment late while the keyboard moves, so the same end
    /// is not asked for again in the meantime.
    @State private var lastJump: (end: CGFloat, at: Date)?

    func body(content: Content) -> some View {
        content
            .defaultScrollAnchor(following ? .bottom : resting, for: .sizeChanges)
            .onScrollGeometryChange(for: LarkFollow.Geometry.self) { g in
                LarkFollow.Geometry(offset: g.contentOffset.y, content: g.contentSize.height, container: g.containerSize.height,
                                    top: g.contentInsets.top, bottom: g.contentInsets.bottom)
            } action: { old, new in
                geometry = new
                settle(from: old)
            }
            .onScrollPhaseChange { old, new in
                phase = new
                switch new {
                case .tracking, .interacting:
                    following = false // the user has the list
                case .idle:
                    // A scroll of the user's that ends at the end follows from there (not one of ours that the
                    // keyboard cut short: no reason to stop following).
                    if old != .animating, let geometry { following = LarkFollow.atEnd(geometry) }
                    settle(from: nil)
                default:
                    break
                }
            }
            .onChange(of: following) { _, now in if now { settle(from: nil) } }
            .onChange(of: active) { _, _ in settle(from: nil) }
    }

    private func settle(from old: LarkFollow.Geometry?) {
        // Not while the user's finger or flick has the list.
        guard active, phase == .idle || phase == .animating, let g = geometry,
              LarkFollow.goesToEnd(old: old, new: g, following: following) else { return }
        let end = LarkFollow.endOffset(g)
        if let last = lastJump, abs(last.end - end) <= LarkFollow.slack, Date.now.timeIntervalSince(last.at) < 0.5 { return }
        lastJump = (end, .now)
        position.scrollTo(edge: .bottom)
    }
}
