import CoreGraphics
import Foundation

/// Two fingers on the Mac's screen turned into its wheel (#4878), in notches (> 0 up: the fingers going down show what
/// is above, as everywhere on iOS and on a Mac with natural scrolling).
/// - A notch every `step` points the fingers travel over the picture as fitted (more when zoomed in: the same notch
///   moves the picture further), the first at half of that, so a small move scrolls too; what is left over stays for
///   the next move and the fingers going back take it back.
/// - Let go fast, it coasts: the speed dies away as a UIScrollView's does, and no more than `coastMost` notches, so a
///   flick does not run off to the end.
nonisolated struct VNCWheel {
    /// Points of finger travel, on the picture as fitted, for a notch. It was 24 (before #4878): 100 pt, 4 notches.
    static let step: CGFloat = 8
    /// The slowest letting go (points a second) that coasts.
    static let flick: CGFloat = 300
    /// What is left of the coasting speed after a millisecond (UIScrollView's normal deceleration is 0.998).
    static let decay: CGFloat = 0.997
    /// The most notches a coast adds.
    static let coastMost: CGFloat = 30
    /// Below this (notches a second) the coast stops.
    static let stopSpeed: CGFloat = 4

    /// Notches the fingers and the coast have come to since the touch began, and how many of them were sent.
    private var total: CGFloat = 0
    private var sent = 0
    /// The coast's speed, in notches a second; 0: not coasting.
    private(set) var speed: CGFloat = 0

    var coasting: Bool { speed != 0 }

    /// Fingers down: what was left over and any coast go.
    mutating func began() {
        self = VNCWheel()
    }

    /// The fingers moved dy points down (up if < 0), the picture zoomed in by zoom (1: fitted): the notches to send.
    mutating func moved(_ dy: CGFloat, zoom: CGFloat = 1) -> Int {
        total += dy / (Self.step * max(zoom, 1))
        return due()
    }

    /// The fingers let go at velocity points a second: coasts if that is fast enough.
    mutating func released(velocity: CGFloat, zoom: CGFloat = 1) {
        guard abs(velocity) >= Self.flick else { return }
        var v = velocity / (Self.step * max(zoom, 1))
        // A coast at v goes v·τ notches in all, τ = −1 / ln(decay) ms.
        let tau = -1 / log(Self.decay) / 1000
        if abs(v) * tau > Self.coastMost { v = (v < 0 ? -1 : 1) * Self.coastMost / tau }
        speed = v
    }

    /// dt seconds of coasting: the notches to send.
    mutating func tick(_ dt: TimeInterval) -> Int {
        guard coasting else { return 0 }
        let after = speed * pow(Self.decay, CGFloat(dt) * 1000)
        // The distance the dying speed covers over dt, exactly: (v₀ − v₁)·τ.
        total += (speed - after) * (-1 / log(Self.decay) / 1000)
        speed = abs(after) < Self.stopSpeed ? 0 : after
        return due()
    }

    mutating func stop() { speed = 0 }

    /// Whole notches not yet sent: the nearest to where it is, so the first comes at half a step.
    private mutating func due() -> Int {
        let n = Int(total.rounded()) - sent
        sent += n
        return n
    }
}
