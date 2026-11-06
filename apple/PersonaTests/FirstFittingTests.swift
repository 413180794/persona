import SwiftUI
import Testing
@testable import Persona

/// `FirstFitting` draws what `ViewThatFits(in: .horizontal)` draws (#1337): the same candidate at every width, to the
/// pixel, and picks the way it does.
@MainActor struct FirstFittingTests {
    @Test func picksLikeViewThatFits() {
        #expect(FirstFitting.pick([120, 60], width: 200) == 0)
        #expect(FirstFitting.pick([120, 60], width: 120) == 0)
        #expect(FirstFitting.pick([120, 60], width: 100) == 1)
        #expect(FirstFitting.pick([120, 60], width: 30) == 1) // none fits: the last, cut short
        #expect(FirstFitting.pick([120, 60], width: nil) == 0)
        #expect(FirstFitting.pick([120, 60], width: .infinity) == 0)
        #expect(FirstFitting.pick([], width: 100) == 0)
    }

    @Test func drawsWhatViewThatFitsDraws() throws {
        let long = "工作中 12 分 · MacBook · persona › main", short = "工作中 12 分 · persona › main"
        func line(_ s: String) -> some View { Text(s).font(Theme.Font.label).lineLimit(1) }
        for width: CGFloat in [400, 260, 200, 150, 80] {
            let a = try png(ViewThatFits(in: .horizontal) { line(long); line(short) }, width: width)
            let b = try png(FirstFitting { line(long); line(short) }, width: width)
            #expect(a == b, "differs at \(width) pt")
        }
    }

    private func png(_ v: some View, width: CGFloat) throws -> Data {
        let r = ImageRenderer(content: v.frame(width: width, alignment: .leading).background(Color.white))
        r.scale = 2
        return try #require(r.uiImage?.pngData())
    }
}
