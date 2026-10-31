import Foundation
import Testing
@testable import Jarvis

struct APIErrorTests {
    /// An id the caller escaped (a 知识库 project slug with Chinese in it) reaches the hub once escaped, not twice (404).
    @MainActor @Test func escapedIdsAreNotEscapedAgain() throws {
        let cfg = try #require(ServerConfig(pairURL: "jarvis://pair?url=http%3A%2F%2Fhub.example.com%3A18660&token=t"))
        let api = APIClient(config: cfg)
        let id = "gsb-打分平台".addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(["/"]))!
        #expect(api.url("/api/v1/kb/projects/\(id)").absoluteString
                == "http://hub.example.com:18660/api/v1/kb/projects/gsb-%E6%89%93%E5%88%86%E5%B9%B3%E5%8F%B0")
        #expect(api.url("/api/v1/kb/projects/a%2Fb").path(percentEncoded: true) == "/api/v1/kb/projects/a%2Fb")
        // Raw paths are still escaped, and queries still work.
        #expect(api.url("/api/v1/files/a b", query: ["q": "x y"]).absoluteString
                == "http://hub.example.com:18660/api/v1/files/a%20b?q=x%20y")
    }

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
