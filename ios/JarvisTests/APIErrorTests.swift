import Foundation
import Testing
@testable import Jarvis

struct APIErrorTests {
    @Test func serverErrorsNameTheRequest() {
        let e = APIError(status: 502, message: "node exited", requestID: "1a2b3c4d")
        #expect(e.errorDescription == "node exited（请求 1a2b3c4d）")
    }

    @Test func answersAreShownAsTheyAre() {
        // A refusal or a missing item is the hub's answer, not a failure to look up.
        let e = APIError(status: 409, message: "电脑上的输入框里有字", fields: ["code": "input_draft"], requestID: "1a2b3c4d")
        #expect(e.errorDescription == "电脑上的输入框里有字")
        #expect(e.code == "input_draft")
    }

    @Test func readsStringFields() {
        let data = Data(#"{"error":"x","code":"input_draft","delivered":"none","n":3}"#.utf8)
        let f = APIError.fields(in: data)
        #expect(f["code"] == "input_draft" && f["delivered"] == "none" && f["error"] == "x")
    }
}
