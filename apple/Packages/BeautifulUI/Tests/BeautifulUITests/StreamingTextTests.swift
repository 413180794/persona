import SwiftUI
import Testing
@testable import BeautifulUI

@Suite struct StreamingTextTests {
    let sources = [StreamingSource(name: "Scoop Data", domain: "scoopdata.io", avatar: .symbol("circle", tint: .green))]

    @Test func wordsKeepTheirTrailingSpace() {
        #expect(StreamingToken.words("up 23% this") == [.text("up "), .text("23% "), .text("this ")])
    }

    @Test func runsMergeTextButKeepCitations() {
        let tokens: [StreamingToken] = [.text("a "), .text("b "), .citation(0), .text("c ")]
        #expect(StreamingText.runs(tokens) == [.text("a b "), .citation(0), .text("c ")])
    }

    @Test func citationsReadAsDomains() {
        let tokens: [StreamingToken] = [.text("Up 23%. "), .citation(0), .citation(5), .text("Done.")]
        #expect(StreamingText.plainText(tokens, sources: sources) == "Up 23%. (scoopdata.io) Done.")
    }
}
