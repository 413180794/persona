import Foundation
import Testing
@testable import Persona

/// Settings › Devices › Connect New Device (#3077): the code the hub makes decodes, the browser link asks for the hub's own page,
/// the countdown, expiry and a new code, and the device that took the code is the credential that was not there
/// before. Made-up values.
@MainActor
struct ConnectDeviceModelTests {
    private static let tokens = #"{"tokens":[{"id":"d1","name":"我的 iPhone","kind":"phone","role":"full"}],"current":"d1"}"#
    private static let tokensAfter = #"""
    {"tokens":[{"id":"d1","name":"我的 iPhone","kind":"phone","role":"full"},
               {"id":"old","name":"旧手机","kind":"phone","role":"full","revoked_at":"2026-11-01T00:00:00Z"},
               {"id":"d2","name":"新 Mac","kind":"desktop","role":"full"}],"current":"d1"}
    """#
    private static let start = Date(timeIntervalSince1970: 1_800_000_000)

    private static func code(expires: Date) -> String {
        let at = ISO8601DateFormatter().string(from: expires)
        return #"{"token":"jp_example","expires_at":"\#(at)","url":"https://hub.example.com","#
            + #""pair_url":"persona://pair?url=https%3A%2F%2Fhub.example.com&token=jp_example","#
            + #""qr":{"size":2,"rows":["10","01"]},"web_url":"https://fake.invalid/#token=jp_example","web_qr":{"size":2,"rows":["11","00"]}}"#
    }

    /// A clock the test moves by hand.
    @MainActor final class Clock { var now = Date(timeIntervalSince1970: 1_800_000_000) }

    private func model(_ hub: FakeHub, _ clock: Clock) -> ConnectDeviceModel {
        ConnectDeviceModel(deps: .init(api: { hub.client }, now: { clock.now }))
    }

    @Test func makesACodeForTheHubsOwnPage() async throws {
        let hub = FakeHub(), clock = Clock()
        hub.on("GET", "/api/v1/auth/tokens", json: Self.tokens)
        hub.on("POST", "/api/v1/auth/pair-codes", json: Self.code(expires: Self.start.addingTimeInterval(600)))
        let page = model(hub, clock)
        await page.make()
        let c = try #require(page.code)
        #expect(c.token == "jp_example" && c.pairLink.hasPrefix("persona://pair?") && c.qr?.rows == ["10", "01"])
        #expect(c.webURL == "https://fake.invalid/#token=jp_example" && c.webQR?.rows == ["11", "00"])
        #expect(page.secondsLeft == 600 && page.live && !page.expired)
        let body = try #require(hub.sent("POST", "/api/v1/auth/pair-codes").first?.body)
        let sent = try JSONSerialization.jsonObject(with: body) as? [String: String]
        #expect(sent == ["web": "https://fake.invalid/"])
    }

    @Test func countsDownExpiresAndMakesANewOne() async throws {
        let hub = FakeHub(), clock = Clock()
        hub.on("GET", "/api/v1/auth/tokens", json: Self.tokens)
        hub.on("POST", "/api/v1/auth/pair-codes", json: Self.code(expires: Self.start.addingTimeInterval(90)))
        let page = model(hub, clock)
        await page.make()
        clock.now = Self.start.addingTimeInterval(30.4)
        page.tick()
        #expect(page.secondsLeft == 60 && ConnectDeviceText.clock(page.secondsLeft) == "1:00")
        clock.now = Self.start.addingTimeInterval(91)
        page.tick()
        #expect(page.expired && !page.live && page.secondsLeft == 0)
        // No polling once it has expired, even if a device shows up.
        hub.on("GET", "/api/v1/auth/tokens", json: Self.tokensAfter)
        #expect(await page.poll() == false)
        hub.on("POST", "/api/v1/auth/pair-codes", json: Self.code(expires: clock.now.addingTimeInterval(600)))
        await page.make()
        #expect(page.live && page.secondsLeft == 600)
        #expect(hub.sent("POST", "/api/v1/auth/pair-codes").count == 2)
    }

    @Test func theNewCredentialIsTheDeviceThatPaired() async throws {
        let hub = FakeHub(), clock = Clock()
        hub.on("GET", "/api/v1/auth/tokens", json: Self.tokens)
        hub.on("POST", "/api/v1/auth/pair-codes", json: Self.code(expires: Self.start.addingTimeInterval(600)))
        let page = model(hub, clock)
        await page.make()
        #expect(await page.poll() == false) // nobody yet
        hub.on("GET", "/api/v1/auth/tokens", json: Self.tokensAfter)
        #expect(await page.poll())
        #expect(page.phase == .paired("新 Mac") && page.code == nil && !page.live)
    }

    @Test func aRefusalShowsTheHubsWords() async {
        let hub = FakeHub(), clock = Clock()
        hub.on("GET", "/api/v1/auth/tokens", json: Self.tokens)
        hub.on("POST", "/api/v1/auth/pair-codes") { _ in throw APIError(status: 403, message: "这个凭据不能生成配对码") }
        let page = model(hub, clock)
        await page.make()
        guard case .failed(let m) = page.phase else { Issue.record("not failed: \(page.phase)"); return }
        #expect(m.contains("这个凭据不能生成配对码"))
    }

    @Test func anOlderHubWithoutQRStillGivesTheCode() async throws {
        let hub = FakeHub(), clock = Clock()
        hub.on("GET", "/api/v1/auth/tokens", json: Self.tokens)
        let at = ISO8601DateFormatter().string(from: Self.start.addingTimeInterval(600))
        hub.on("POST", "/api/v1/auth/pair-codes", json: #"{"token":"jp_example","expires_at":"\#(at)","pair_url":"persona://pair?token=jp_example"}"#)
        let page = model(hub, clock)
        await page.make()
        let c = try #require(page.code)
        #expect(c.qr == nil && c.webURL == nil && c.token == "jp_example")
    }

    @Test func pageAddressAndLoopback() throws {
        let u = { (s: String) in URL(string: s)! }
        #expect(ConnectDeviceText.webBase(u("https://hub.example.com")) == "https://hub.example.com/")
        #expect(ConnectDeviceText.webBase(u("https://hub.example.com:8443/persona?x=1")) == "https://hub.example.com:8443/persona/")
        #expect(ConnectDeviceText.webBase(u("http://hub.example.com:18660/")) == "http://hub.example.com:18660/")
        #expect(ConnectDeviceText.webBase(u("ssh://hub")) == nil)
        #expect(ConnectDeviceText.loopbackOnly(u("http://127.0.0.1:18660")))
        #expect(ConnectDeviceText.loopbackOnly(u("http://localhost:18660")))
        #expect(ConnectDeviceText.loopbackOnly(u("http://[::1]:18660")))
        #expect(!ConnectDeviceText.loopbackOnly(u("https://hub.example.com")))
        #expect(ConnectDeviceText.clock(0) == "0:00" && ConnectDeviceText.clock(599) == "9:59" && ConnectDeviceText.clock(-3) == "0:00")
    }
}
