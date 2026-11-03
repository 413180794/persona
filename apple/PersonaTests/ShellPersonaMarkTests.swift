import Foundation
import Testing
@testable import Jarvis

/// The inbox's mark beside the shell's face (`ShellPersonaButton.markShift`): its bottom level with the face's, nothing
/// below the face (it lay over Agents' search field, 2026-11-02), and clear of the round face by the gap, at every text
/// size.
struct ShellPersonaMarkTests {
    @Test(arguments: [1.0, 1.2, 1.6])
    func theMarkClearsTheFaceWithoutHangingBelowIt(scale: CGFloat) {
        let face = 44 * scale, mark = 18 * scale, gap: CGFloat = 2
        let shift = ShellPersonaButton.markShift(face: face, mark: mark, gap: gap)
        // Placed at the face frame's bottomTrailing, moved `shift` to the right.
        let markCenter = CGPoint(x: face - mark / 2 + shift, y: face - mark / 2)
        let faceCenter = CGPoint(x: face / 2, y: face / 2)
        let apart = hypot(markCenter.x - faceCenter.x, markCenter.y - faceCenter.y)
        #expect(abs(apart - (face / 2 + mark / 2 + gap)) < 0.001, "the mark is \(apart) from the face's center")
        #expect(markCenter.y + mark / 2 <= face, "the mark reaches below the face")
        #expect(shift > 0, "the mark is over the face")
    }
}
