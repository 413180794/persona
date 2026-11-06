import Foundation
import Testing
@testable import Persona

/// The inbox's dot on the shell's face (`ShellPersonaButton.dotShift`): on the face's lower right at 45°, inside the
/// round face (nothing past it, nothing below the face: the old mark lay over Agents' search field, 2026-11-02), at
/// every text size.
struct ShellPersonaMarkTests {
    @Test(arguments: [1.0, 1.2, 1.6])
    func theDotSitsOnTheFaceInsideItsRim(scale: CGFloat) {
        let face = 44 * scale, dot = 10 * scale
        let shift = ShellPersonaButton.dotShift(face: face, dot: dot)
        // Placed at the face frame's bottomTrailing, moved `shift` right and down (up and left: it is negative).
        let dotCenter = CGPoint(x: face - dot / 2 + shift, y: face - dot / 2 + shift)
        let faceCenter = CGPoint(x: face / 2, y: face / 2)
        let apart = hypot(dotCenter.x - faceCenter.x, dotCenter.y - faceCenter.y)
        #expect(abs(apart - (face / 2 - dot / 2)) < 0.001, "the dot is \(apart) from the face's center")
        #expect(dotCenter.x > faceCenter.x && dotCenter.y > faceCenter.y, "the dot is not on the lower right")
        #expect(shift < 0, "the dot hangs past the face")
    }
}
