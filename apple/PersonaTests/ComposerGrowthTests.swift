import Testing
@testable import Jarvis

struct ComposerGrowthTests {
    @Test func smallAndLargeKeyboardViewports() {
        #expect(ComposerGrowth.maximumLines(viewportHeight: 360, lineHeight: 24) == 3)
        #expect(ComposerGrowth.maximumLines(viewportHeight: 600, lineHeight: 24) == 5)
    }

    @Test func landscapeAndLargeTypeKeepTheDraftBounded() {
        #expect(ComposerGrowth.maximumLines(viewportHeight: 150, lineHeight: 24) == 3)
        #expect(ComposerGrowth.maximumLines(viewportHeight: 300, lineHeight: 40) == 3)
        #expect(ComposerGrowth.maximumLines(viewportHeight: 1400, lineHeight: 24) == 8)
        #expect(ComposerGrowth.maximumLines(viewportHeight: 0, lineHeight: 0) == 3)
    }

    @Test func expandStartsAtTheLimitAndDisappearsBelowIt() {
        for limit in 3...8 {
            #expect(!ComposerGrowth.offersExpansion(textHeight: Double(limit - 1) * 24, lineHeight: 24, maximumLines: limit))
            #expect(ComposerGrowth.offersExpansion(textHeight: Double(limit) * 24, lineHeight: 24, maximumLines: limit))
            #expect(ComposerGrowth.offersExpansion(textHeight: 1000, lineHeight: 24, maximumLines: limit))
        }
        #expect(!ComposerGrowth.offersExpansion(textHeight: 100, lineHeight: 0, maximumLines: 3))
    }
}
