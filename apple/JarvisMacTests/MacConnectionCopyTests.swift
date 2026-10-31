import Testing
@testable import Jarvis

/// The sidebar's connection footer explains itself with the shared copy; on the Mac it speaks of this Mac.
@MainActor
struct MacConnectionCopyTests {
    @Test func offlineSpeaksOfTheMac() {
        let s = ConnectionDetails.explanation(.waitingForNetwork, stalled: false)
        #expect(s.contains("这台 Mac"))
        #expect(!s.contains("iPhone"))
    }
}
