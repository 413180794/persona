import Foundation
import Testing
@testable import Jarvis

@MainActor struct FeishuBotModelTests {
    private static let status = """
    {"enabled":true,"app_id":"cli_a","domain":"https://open.feishu.cn","has_secret":true,"from_config":false,
     "features":{"main":true,"cards":true,"all_cards":false,"pushes":false,"sync":false,"agents":false},
     "connected":true,"bot_name":"Jarvis","owner":{"name":"我","paired_at":1}}
    """

    private func model(_ hub: FakeHub, toasts: BotRecorder<[String]> = BotRecorder([])) -> FeishuBotModel {
        FeishuBotModel(deps: .init(api: { hub.client }, toast: { toasts.value.append($0.message) }))
    }

    @Test func loadsAndSummarizes() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/bots/feishu", json: Self.status)
        let m = model(hub)
        await m.load()
        #expect(m.status?.botName == "Jarvis")
        #expect(m.appID == "cli_a" && m.domain == "feishu" && m.appSecret.isEmpty)
        #expect(!m.credentialsChanged)
        #expect(m.stateText == "已连接")
        #expect(FeishuBotModel.summary(m.status) == "已连接")
        var unpaired = try #require(m.status)
        unpaired.owner = nil
        #expect(FeishuBotModel.summary(unpaired) == "待配对")
        unpaired.enabled = false
        #expect(FeishuBotModel.summary(unpaired) == "未打开")
        #expect(FeishuBotModel.domainChoice("https://lark-connectorsuite.com") == "lark")
    }

    /// A switch sends every feature (the hub replaces them as a whole); "all cards" brings "cards" along, and turning
    /// "cards" off takes "all cards" with it.
    @Test func featuresGoAsAWhole() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/bots/feishu", json: Self.status)
        var bodies: [[String: Any]] = []
        hub.on("PUT", "/api/v1/bots/feishu") { req in
            bodies.append(try JSONSerialization.jsonObject(with: req.body ?? Data()) as? [String: Any] ?? [:])
            return Data(Self.status.utf8)
        }
        let m = model(hub)
        await m.load()
        await m.set(\.cards, false)
        var f = try #require(bodies.last?["features"] as? [String: Bool])
        #expect(f["cards"] == false && f["all_cards"] == false && f["main"] == true)
        await m.set(\.allCards, true)
        f = try #require(bodies.last?["features"] as? [String: Bool])
        #expect(f["cards"] == true && f["all_cards"] == true)
        #expect(bodies.allSatisfy { $0["app_secret"] == nil })
    }

    /// Saving credentials sends the secret once; what is typed is kept while a load arrives, and the hub's error shows.
    @Test func savesCredentialsAndShowsTheHubsError() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/bots/feishu", json: Self.status)
        let toasts = BotRecorder<[String]>([])
        let m = model(hub, toasts: toasts)
        await m.load()
        m.appID = "cli_b"
        m.appSecret = "sec"
        await m.load()
        #expect(m.appID == "cli_b" && m.appSecret == "sec") // not overwritten while typing
        hub.on("PUT", "/api/v1/bots/feishu") { _ in throw APIError(status: 400, message: "连不上这个应用") }
        await m.saveCredentials()
        #expect(m.error?.contains("连不上这个应用") == true)
        #expect(m.appSecret == "sec")
        hub.on("PUT", "/api/v1/bots/feishu") { req in
            let body = try JSONSerialization.jsonObject(with: req.body ?? Data()) as? [String: Any]
            #expect(body?["app_id"] as? String == "cli_b" && body?["app_secret"] as? String == "sec" && body?["domain"] as? String == "feishu")
            return Data(Self.status.replacingOccurrences(of: "cli_a", with: "cli_b").utf8)
        }
        await m.saveCredentials()
        #expect(m.error == nil && m.appSecret.isEmpty && m.appID == "cli_b")
        #expect(toasts.value == ["应用凭证已验证并保存"])
    }
}

/// A reference cell for what a closure records.
final class BotRecorder<T>: @unchecked Sendable {
    var value: T
    init(_ value: T) { self.value = value }
}
