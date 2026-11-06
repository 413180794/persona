import CoreGraphics
import Testing
@testable import Persona

/// Two fingers on the Mac's screen as its wheel (#4878, VNCWheel): how far 100 pt goes, small moves, coasting.
struct VNCWheelTests {
    /// 100 pt in moves of `each` points, as a pan reports them.
    private func drag(_ wheel: inout VNCWheel, _ points: CGFloat, each: CGFloat = 2, zoom: CGFloat = 1) -> Int {
        var notches = 0
        var gone: CGFloat = 0
        while abs(gone) < abs(points) {
            let d = points < 0 ? -each : each
            notches += wheel.moved(d, zoom: zoom)
            gone += d
        }
        return notches
    }

    private func coast(_ wheel: inout VNCWheel) -> (notches: Int, frames: Int) {
        var notches = 0, frames = 0
        while wheel.coasting, frames < 600 {
            notches += wheel.tick(1.0 / 60)
            frames += 1
        }
        return (notches, frames)
    }

    @Test func aHundredPointsIsAboutThirteenNotchesWhicheverWay() {
        var w = VNCWheel()
        #expect(drag(&w, 100) == 13) // before: 4
        w.began()
        #expect(drag(&w, -100) == -13)
        // Zoomed in twice, a notch moves the picture twice as far: half the notches.
        w.began()
        #expect(drag(&w, 100, zoom: 2) == 6)
    }

    @Test func aSmallMoveScrollsAndWhatIsLeftIsKept() {
        var w = VNCWheel()
        #expect(w.moved(3) == 0)
        #expect(w.moved(1) == 1) // half a step: the first notch
        #expect(w.moved(7) == 0)
        #expect(w.moved(1) == 1) // the rest of the first step and a half are kept, not dropped
        // Back the other way takes them back.
        #expect(w.moved(-8) == -1)
    }

    @Test func aSlowLetGoStopsAndAFlickCoastsAWayButNotOffTheEnd() {
        var w = VNCWheel()
        _ = drag(&w, 40)
        w.released(velocity: 120)
        #expect(!w.coasting)

        w.began()
        _ = drag(&w, 40)
        w.released(velocity: 600) // 75 notches a second, dying away in about a third of a second
        let some = coast(&w)
        #expect((20...26).contains(some.notches))
        #expect(some.frames < 90)

        w.began()
        w.released(velocity: -5000) // as hard as a finger goes
        let most = coast(&w)
        #expect(most.notches < 0 && -most.notches <= Int(VNCWheel.coastMost))
    }

    @Test func fingersDownAgainStopTheCoast() {
        var w = VNCWheel()
        w.released(velocity: 2000)
        #expect(w.tick(1.0 / 60) > 0)
        w.began()
        #expect(!w.coasting)
        #expect(w.tick(1.0 / 60) == 0)
    }
}
