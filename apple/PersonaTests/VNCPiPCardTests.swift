import CoreVideo
import Testing
@testable import Persona

/// The small window's card (#1839, VNCPiPCard): what it says for each phase of the connection, and that it draws into a
/// frame of its size (3×) with something on it.
@MainActor struct VNCPiPCardTests {
    @Test func saysWhatTheConnectionIsDoing() {
        #expect(VNCPiPCard.State(.connecting, pictured: false) == .connecting)
        #expect(VNCPiPCard.State(.live, pictured: false) == .connecting) // signed in, the first frame not yet there
        #expect(VNCPiPCard.State(.live, pictured: true) == nil)
        #expect(VNCPiPCard.State(.signIn(failed: nil), pictured: false) == .signIn)
        #expect(VNCPiPCard.State(.closed("连不上 hub"), pictured: true) == .failed("连不上 hub"))
        #expect(VNCPiPCard.State(.closed(nil), pictured: false) == .failed("连接断了"))
    }

    @Test func drawsAFrameOfItsSize() throws {
        let size = CGSize(width: Theme.Size.pictureInPictureCard, height: 150)
        for state in [VNCPiPCard.State.connecting, .signIn, .failed("连接断了")] {
            let pb = try #require(VNCPiPCard.frame(state, name: "MacBook", size: size, turn: 0.25))
            #expect(CVPixelBufferGetWidth(pb) == Int(size.width * 3))
            #expect(CVPixelBufferGetHeight(pb) == Int(size.height * 3))
            #expect(CVPixelBufferGetPixelFormatType(pb) == kCVPixelFormatType_32BGRA)
            #expect(Self.lit(pb) > 0, "\(state): nothing drawn on the black")
        }
    }

    /// How many pixels are not black.
    private static func lit(_ pb: CVPixelBuffer) -> Int {
        CVPixelBufferLockBaseAddress(pb, .readOnly)
        defer { CVPixelBufferUnlockBaseAddress(pb, .readOnly) }
        guard let base = CVPixelBufferGetBaseAddress(pb)?.assumingMemoryBound(to: UInt8.self) else { return 0 }
        let row = CVPixelBufferGetBytesPerRow(pb)
        var n = 0
        for y in 0..<CVPixelBufferGetHeight(pb) {
            for x in 0..<CVPixelBufferGetWidth(pb) where base[y * row + x * 4] > 40 || base[y * row + x * 4 + 1] > 40 || base[y * row + x * 4 + 2] > 40 {
                n += 1
            }
        }
        return n
    }
}
