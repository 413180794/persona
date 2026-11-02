import AppKit
import QuartzCore
import SwiftUI

/// The ring that breathes out of a needs-you dot (`StatusDot`), drawn by Core Animation. The iPhone redraws it from a
/// 30 Hz `TimelineView`; on the Mac every one of those redraws made the hosting view ask for new constraints, and the
/// whole window was laid out again, 30 times a second, even with the screen locked (14% of a core
/// idle on the demo's Agents page, 2026-11-02). Here the render server runs the animation and the app does nothing.
struct MacBreathingRing: NSViewRepresentable {
    var color: Color

    func makeNSView(context: Context) -> MacBreathingRingView { MacBreathingRingView() }

    func updateNSView(_ view: MacBreathingRingView, context: Context) {
        view.ring.strokeColor = color.resolve(in: context.environment).cgColor
    }
}

final class MacBreathingRingView: NSView {
    let ring = CAShapeLayer()
    static let key = "breath"

    override init(frame: NSRect) {
        super.init(frame: frame)
        wantsLayer = true
        ring.fillColor = nil
        ring.lineWidth = StatusDot.ringWidth
        layer?.addSublayer(ring)
    }

    @available(*, unavailable) required init?(coder: NSCoder) { fatalError() }

    override var isFlipped: Bool { true }

    /// Clicks go to the row the dot is in.
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func layout() {
        super.layout()
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        ring.frame = bounds
        ring.path = CGPath(ellipseIn: bounds, transform: nil)
        CATransaction.commit()
    }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        ring.removeAnimation(forKey: Self.key)
        if window != nil { ring.add(Self.animation(at: ring.convertTime(CACurrentMediaTime(), from: nil)), forKey: Self.key) }
    }

    /// One breath a second: out to `1 + breathScale` times the dot while it fades, eased out as the iPhone's
    /// `1 - (1 - p)²` (the cubic below is exactly that curve), at most 30 frames a second as the iPhone draws it.
    /// Every breath starts on a whole second of Core Animation's clock, so every dot in the window breathes together.
    /// Not the wall clock: Core Animation's stops while the Mac sleeps, and dots added after a sleep drifted apart.
    static func animation(at now: CFTimeInterval) -> CAAnimation {
        let scale = CABasicAnimation(keyPath: "transform.scale")
        scale.fromValue = 1
        scale.toValue = 1 + StatusDot.breathScale
        let fade = CABasicAnimation(keyPath: "opacity")
        fade.fromValue = 1
        fade.toValue = 0
        let group = CAAnimationGroup()
        group.animations = [scale, fade]
        group.duration = StatusDot.breath
        group.repeatCount = .infinity
        group.timingFunction = easeOut
        group.beginTime = now - now.truncatingRemainder(dividingBy: StatusDot.breath)
        group.preferredFrameRateRange = CAFrameRateRange(minimum: 10, maximum: 30, preferred: 30)
        group.isRemovedOnCompletion = false
        return group
    }

    /// `1 - (1 - p)²` as a cubic Bézier: the quadratic with control point (0.5, 1), raised to a cubic.
    static let easeOut = CAMediaTimingFunction(controlPoints: 1 / 3, 2 / 3, 2 / 3, 1)
}
