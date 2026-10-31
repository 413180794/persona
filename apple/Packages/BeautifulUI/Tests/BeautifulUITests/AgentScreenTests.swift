import SwiftUI
import Testing
@testable import BeautifulUI

@Suite struct AgentScreenTests {
    @Test func clockPadsMinutesAndSeconds() {
        #expect(AgentScreen<EmptyView>.clock(0) == "00:00")
        #expect(AgentScreen<EmptyView>.clock(75) == "01:15")
        #expect(AgentScreen<EmptyView>.clock(3601) == "60:01")
    }

    @Test func cardKeepsTheCaptureAspect() {
        #expect(abs(AgentScreen<EmptyView>.aspect - 2964.0 / 1856.0) < 0.0001)
    }
}
