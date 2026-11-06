import Foundation
import Testing
@testable import Jarvis

/// 扩展市场: a GitHub market read without a token is the one GitHub's per-network limit stops, so the catalog's
/// "读不到" for it comes with 填 token and why (public repositories included); a token, or another kind of market,
/// has nothing to add.
@MainActor
struct MarketTokenHintTests {
    private func market(spec: String, token: Bool) throws -> ExtensionMarket {
        let json = #"{"id":"m","spec":"\#(spec)","name":"m","has_token":\#(token),"error":"被限流了（HTTP 403）"}"#
        return try JSON.decoder.decode(ExtensionMarket.self, from: Data(json.utf8))
    }

    @Test func gitHubWithoutTokenWantsOne() throws {
        #expect(try market(spec: "github:example/market", token: false).wantsGitHubToken)
        #expect(try market(spec: "github:example/market@dev", token: false).wantsGitHubToken)
    }

    @Test func tokenOrOtherKindDoesNot() throws {
        #expect(try !market(spec: "github:example/market", token: true).wantsGitHubToken)
        #expect(try !market(spec: "https://plugins.example.com/", token: false).wantsGitHubToken)
        #expect(try !market(spec: "git:git@example.com:team/market.git", token: false).wantsGitHubToken)
    }

    /// The catalog asks no token of the official market (docs/onboarding.md 4.5), so it must know which one that is.
    @Test func officialDecodes() throws {
        let json = #"{"id":"o","spec":"github:example/official","name":"官方","has_token":false,"official":true}"#
        #expect(try JSON.decoder.decode(ExtensionMarket.self, from: Data(json.utf8)).official == true)
        #expect(try market(spec: "github:example/market", token: false).official == nil) // a hub before #2351
    }

    @Test func hintSaysPublicReposToo() {
        #expect(ExtensionMarketText.gitHubTokenHint.contains("公开仓库也适用"))
    }
}
