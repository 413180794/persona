import CoreGraphics
import Testing
@testable import Persona

/// How soon the shell's top bar fades as the search field comes up into its row (#4928).
struct AgentsTopBarFadeTests {
    /// The field's way, as on an iPhone 17 Pro: from its row (150) up to the bar's row (62).
    private let field: CGFloat = 150, top: CGFloat = 62

    /// Where the field's top is at `p` of its way.
    private func fieldTop(_ p: Double) -> CGFloat { field - (field - top) * CGFloat(p) }

    @Test func theStandardSizeKeepsItsTiming() {
        #expect(AgentsTopBarFade.gone(bar: top...112.3, field: field) == AgentsTopBarFade.standard)
        #expect(AgentsTopBarFade.gone(bar: nil, field: field) == AgentsTopBarFade.standard)
    }

    @Test func aTallerBarIsGoneBeforeTheFieldGetsToIt() {
        for bottom: CGFloat in [112.3, 121.7, 135] {
            let gone = AgentsTopBarFade.gone(bar: top...bottom, field: field)
            #expect(fieldTop(gone) >= bottom + Theme.Space.sm - 0.01, "the bar down to \(bottom) still there at \(gone)")
        }
        // XXXL, the bar 62–121.7: gone by a quarter of the way (it was 0.35, and the field met it at 0.32).
        #expect(abs(AgentsTopBarFade.gone(bar: top...121.7, field: field) - 0.253) < 0.005)
    }

    @Test func aBarDownToTheFieldGoesAtOnce() {
        #expect(AgentsTopBarFade.gone(bar: top...160, field: field) == 0.05)
    }
}
