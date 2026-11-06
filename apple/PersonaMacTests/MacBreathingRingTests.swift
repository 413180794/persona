import AppKit
import QuartzCore
import Testing
@testable import Persona

/// The needs-you ring on the Mac is a Core Animation animation, the same breath the iPhone draws from its clock.
@MainActor
struct MacBreathingRingTests {
    @Test func breathesLikeTheIPhone() throws {
        let group = try #require(MacBreathingRingView.animation(at: 1000.25) as? CAAnimationGroup)
        #expect(group.duration == StatusDot.breath)
        #expect(group.repeatCount == .infinity)
        // At most the iPhone's 30 frames a second (the render server would run it at the display's 120).
        #expect(group.preferredFrameRateRange.maximum == 30)
        // Every breath starts on a whole second of the clock: a dot added later breathes with the ones already there.
        #expect(group.beginTime == 1000)
        let later = try #require(MacBreathingRingView.animation(at: 1003.7) as? CAAnimationGroup)
        #expect(later.beginTime == 1003)
        let parts = (group.animations ?? []).compactMap { $0 as? CABasicAnimation }
        let scale = try #require(parts.first { $0.keyPath == "transform.scale" })
        #expect(scale.fromValue as? Int == 1)
        #expect((scale.toValue as? CGFloat).map { abs($0 - 2.2) < 1e-9 } == true)
        let fade = try #require(parts.first { $0.keyPath == "opacity" })
        #expect(fade.fromValue as? Int == 1)
        #expect(fade.toValue as? Int == 0)
    }

    /// The timing curve is the iPhone's `1 - (1 - p)²`, not an approximation of it.
    @Test func easesOutAsTheIPhone() {
        var c: [Float] = [0, 0]
        var p1 = CGPoint.zero, p2 = CGPoint.zero
        MacBreathingRingView.easeOut.getControlPoint(at: 1, values: &c); p1 = CGPoint(x: CGFloat(c[0]), y: CGFloat(c[1]))
        MacBreathingRingView.easeOut.getControlPoint(at: 2, values: &c); p2 = CGPoint(x: CGFloat(c[0]), y: CGFloat(c[1]))
        for i in 0...20 {
            let s = CGFloat(i) / 20
            let u = 1 - s
            let x = 3 * u * u * s * p1.x + 3 * u * s * s * p2.x + s * s * s
            let y = 3 * u * u * s * p1.y + 3 * u * s * s * p2.y + s * s * s
            #expect(abs(y - (1 - (1 - x) * (1 - x))) < 1e-6)
        }
    }

    /// The ring is drawn: on the view's layer, round, as big as the dot.
    @Test func drawsARingTheSizeOfTheDot() {
        let view = MacBreathingRingView(frame: NSRect(x: 0, y: 0, width: 9, height: 9))
        view.layout()
        #expect(view.ring.superlayer === view.layer)
        #expect(view.ring.frame == view.bounds)
        #expect(view.ring.path?.boundingBox == view.bounds)
        #expect(view.ring.lineWidth == StatusDot.ringWidth)
        #expect(view.ring.fillColor == nil)
    }

    /// In a window it is running; out of one it is not (nothing left to run when the row goes away).
    @Test func runsOnlyInAWindow() {
        let view = MacBreathingRingView(frame: NSRect(x: 0, y: 0, width: 9, height: 9))
        #expect(view.ring.animation(forKey: MacBreathingRingView.key) == nil)
        let window = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 40, height: 40), styleMask: [], backing: .buffered, defer: true)
        window.contentView?.addSubview(view)
        #expect(view.ring.animation(forKey: MacBreathingRingView.key) != nil)
        view.removeFromSuperview()
        #expect(view.ring.animation(forKey: MacBreathingRingView.key) == nil)
        // Clicks go through to the row.
        #expect(view.hitTest(NSPoint(x: 4, y: 4)) == nil)
    }
}
