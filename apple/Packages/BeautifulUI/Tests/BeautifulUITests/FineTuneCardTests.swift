import Testing
@testable import BeautifulUI

@Suite struct FineTuneFieldTests {
    let radius = FineTuneField(id: "radius", label: "Radius", value: 28, range: 0...64)

    @Test func dragsOneStepPerTwoPoints() {
        #expect(radius.scrubbed(from: 28, dx: 20) == 38)
        #expect(radius.scrubbed(from: 28, dx: -9) == 24)
    }

    @Test func clampsToItsRange() {
        #expect(radius.scrubbed(from: 28, dx: 400) == 64)
        #expect(radius.scrubbed(from: 28, dx: -400) == 0)
        #expect(radius.clamp(-5) == 0)
    }
}
