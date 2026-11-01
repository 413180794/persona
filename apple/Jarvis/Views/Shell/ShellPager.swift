import SwiftUI

/// The shell's two pages side by side, 助手 on the left and Agents on the right like the switch over them
/// (docs/ios.md「新外壳」): a drag to the left on 助手 brings Agents in under the finger, a drag to the right on Agents
/// brings 助手 back, and the switch's pill follows. The whole strip reads 抽屉 | 助手 | Agents: on 助手 a drag to the
/// right still opens the drawer (`DrawerPan` decides which one a drag is).
///
/// Its own object, like `AssistantDrawer`: while the finger moves only the pages' slide, the top bars' pin and the
/// switch's pill read the position, never the pages themselves.
@MainActor @Observable final class ShellPager {
    /// 0 = 助手 in full, 1 = Agents in full; in between while dragging or settling.
    private(set) var position: CGFloat = 0
    /// A drag or a settle is under way: both pages are on screen until it ends.
    private(set) var moving = false
    /// The page's width (`ShellView` measures it).
    @ObservationIgnored var width: CGFloat = 390
    @ObservationIgnored private var dragStart: CGFloat = 0
    /// Each settle's number: only the last one's end clears `moving`.
    @ObservationIgnored private var serial = 0
    /// Where a drag settled: the shell selects that page.
    @ObservationIgnored var settled: (Int) -> Void = { _ in }
    /// A drag took hold: the shell builds Agents if it has not yet.
    @ObservationIgnored var began: () -> Void = {}

    /// The page at rest (or nearest).
    var page: Int { position < 0.5 ? 0 : 1 }

    func dragBegan() {
        serial += 1
        dragStart = position
        moving = true
        began()
    }

    /// The finger is `x` points to the right of where the drag took hold: the strip moves the other way. Straight to
    /// the value, no animation; a little resistance past either end.
    func drag(_ x: CGFloat) {
        var p = dragStart - x / max(width, 1)
        if p < 0 { p /= 3 } else if p > 1 { p = 1 + (p - 1) / 3 }
        var t = Transaction()
        t.disablesAnimations = true
        withTransaction(t) { position = p }
    }

    /// The finger let go at `velocity` points per second (to the right is positive): the same rule as the drawer and
    /// the system's back swipe, a flick goes its way, otherwise past half way.
    func letGo(velocity: CGFloat) {
        let from = dragStart.rounded()
        let toward = from == 0 ? 1.0 : 0.0
        // How far along towards the other page, and the finger's speed in that direction.
        let along = abs(position - from)
        let speed = from == 0 ? -velocity : velocity
        let goes = SwipeRelease.goes(progress: along, velocity: speed, width: max(width, 1))
        let target = goes ? toward : from
        if goes { Haptics.tap() }
        settle(at: target, velocity: velocity)
        if goes { settled(Int(target)) }
    }

    /// To `page` (0 or 1): the switch was tapped, or something routed to the other page. Nothing to do when a drag
    /// already settled there (its target is the position already).
    func slide(to page: Int, animated: Bool = true) {
        let target = CGFloat(page)
        guard position != target else { return }
        serial += 1
        guard animated else {
            position = target
            moving = false
            return
        }
        if moving {
            settle(at: target, velocity: 0)
        } else {
            // At rest the other page lies under this one (`ShellPageSlide`): put it beside first, then slide.
            moving = true
            let mine = serial
            DispatchQueue.main.async { [weak self] in
                guard let self, serial == mine else { return }
                settle(at: target, velocity: 0)
            }
        }
    }

    #if DEBUG
    /// `-showShellPagerAt <0…1>`: the strip held part way, as under a finger, for screenshots of a drag.
    func hold(at p: CGFloat) {
        moving = true
        position = p
    }
    #endif

    /// The drag was taken away (a call, the system): back to where it started.
    func cancel() {
        settle(at: dragStart.rounded(), velocity: 0)
    }

    private func settle(at target: CGFloat, velocity: CGFloat) {
        let way = (target - position) * max(width, 1)
        // The spring's initial velocity is a share of the way per second (the strip moves against the finger).
        let share = abs(way) > 1 ? max(0, Double(-velocity / way)) : 0
        let motion = Theme.Motion.drawer(velocity: share)
        let mine = serial
        moving = true
        withAnimation(motion) {
            position = target
        } completion: { [weak self] in
            guard let self, serial == mine else { return }
            moving = false
        }
    }
}

/// Where a drag that follows the finger lets go, like the system's back swipe: a flick goes its way, otherwise past half
/// way (counting a little of where it is heading) goes. The drawer and the pager both use it. `progress` 0…1 along the
/// way, `velocity` in points per second, positive towards the end.
enum SwipeRelease {
    nonisolated static func goes(progress: CGFloat, velocity: CGFloat, width: CGFloat) -> Bool {
        if abs(velocity) > 300 { return velocity > 0 }
        return progress + velocity * 0.2 / max(width, 1) > 0.5
    }
}

/// One of the shell's pages on the strip: beside the other one while the strip moves, alone and in reach at rest. At
/// rest the hidden page lies under the shown one (transparent, out of reach), not off the screen: UI tests find a
/// page's controls by id, and the first match off screen could not be tapped (`ShellTests`).
struct ShellPageSlide: ViewModifier {
    let pager: ShellPager
    /// 0 = 助手, 1 = Agents.
    let index: Int

    func body(content: Content) -> some View {
        let p = pager.position
        let shown = pager.moving ? abs(CGFloat(index) - p) < 1 : index == pager.page
        let resting = !pager.moving && index == pager.page
        content
            .offset(x: pager.moving ? (CGFloat(index) - p) * pager.width : 0)
            .opacity(shown ? 1 : 0)
            .allowsHitTesting(resting)
            .accessibilityHidden(!resting)
            .zIndex(index == pager.page ? 1 : 0)
    }
}

/// A page's own top bar steps aside while the strip moves: `ShellMovingBar` stands in for both (drawn in its page, each
/// bar was covered by the other page where it reached over it).
struct ShellBarAtRest: ViewModifier {
    @Environment(\.shell) private var shell

    func body(content: Content) -> some View {
        if let pager = shell?.pager {
            content.opacity(pager.moving ? 0 : 1)
        } else {
            content
        }
    }
}
