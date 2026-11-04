import Foundation
import Testing
@testable import Jarvis

/// The live screen's address (ScreenViewer.url, #1498): this device's token reaches the page as it is. It went out
/// encoded twice (a `jd_` token as `jd%255F`), the page read `jd%5F…`, and the hub said invalid token.
struct ScreenViewerTests {
    /// The fragment's values as the page reads them (URLSearchParams: %XX decoded once, a bare + read as a space).
    private func read(_ url: URL) -> [String: String] {
        let fragment = URLComponents(url: url, resolvingAgainstBaseURL: false)?.percentEncodedFragment ?? ""
        var out: [String: String] = [:]
        for pair in fragment.split(separator: "&") {
            let kv = pair.split(separator: "=", maxSplits: 1).map { String($0).replacingOccurrences(of: "+", with: " ").removingPercentEncoding ?? "" }
            out[kv[0]] = kv.count > 1 ? kv[1] : ""
        }
        return out
    }

    @Test func thePageGetsTheDeviceTokenAsItIs() throws {
        let page = try #require(URL(string: "http://127.0.0.1:18660/screen/macstudio"))
        for token in ["jd_cafe00a1", "a+b/c=&d%e~f.g-h"] {
            let url = try #require(ScreenViewer.url(page, token: token, name: "Mac Studio"))
            let got = read(url)
            #expect(got["token"] == token, "\(url.absoluteString)")
            #expect(got["name"] == "Mac Studio" && got["embed"] == "1", "\(url.absoluteString)")
            #expect(url.path == "/screen/macstudio" && url.query == nil, "the token stays out of what is sent to the hub")
        }
        let plain = try #require(ScreenViewer.url(page, token: "jd_cafe00a1", name: nil))
        #expect(plain.absoluteString == "http://127.0.0.1:18660/screen/macstudio#token=jd_cafe00a1&embed=1")
    }
}
