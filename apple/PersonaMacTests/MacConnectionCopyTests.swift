import Foundation
import Testing
@testable import Persona

/// The shared copy that explains the hub being out of reach; on the Mac it speaks of this Mac.
@MainActor
struct MacConnectionCopyTests {
    @Test func offlineSpeaksOfTheMac() {
        let s = ConnectionDetails.explanation(.waitingForNetwork, stalled: false)
        #expect(s.contains("这台 Mac"))
        #expect(!s.contains("iPhone"))
    }

    @Test func feishuDocHubUnreachableSpeaksOfTheMac() {
        let s = URLError(.cannotFindHost).feishuDocsMessage ?? ""
        #expect(s.contains("这台 Mac"))
        #expect(!s.contains("手机"))
    }
}
