import Foundation
import Testing
@testable import Jarvis

/// The Agents rows' 草稿 (#4578): a chat with text or quotes not sent; blank text alone is none.
struct DraftMarksTests {
    private let a = SessionKey(machineID: "m", sessionID: "a")
    private let b = SessionKey(machineID: "m", sessionID: "b")
    private let c = SessionKey(machineID: "m", sessionID: "c")

    @Test func textOrQuotesMakeADraft() {
        let quote = ContextRef(kind: "quote", title: "Claude", url: "", text: "上一条")
        let marks = AppModel.marks([a: "继续", b: "  \n", c: ""], [b: [quote], c: []])
        #expect(marks == [a, b])
    }

    /// A quote is saved as the text is: it comes back after the app was killed.
    @Test func aQuoteRoundTrips() throws {
        let quote = ContextRef(kind: "quote", title: "Claude", url: "", text: "上一条", msg: "42")
        let back = try JSONDecoder().decode(ContextRef.self, from: JSONEncoder().encode(quote))
        #expect(back == quote)
    }
}
