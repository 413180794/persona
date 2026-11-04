import Foundation
import Testing
@testable import Jarvis

/// A botmux session's reason for waiting and its Feishu topic (attention_reason, external_url; docs/engines.md「botmux」).
@MainActor
struct SessionAttentionTests {
    private func session(_ json: String) throws -> Session {
        try JSON.decoder.decode(Session.self, from: Data(json.utf8))
    }

    @Test func theReasonTakesTheListLineAndTheTopicOpensOnTheWeb() throws {
        let s = try session(#"{"session_id":"s","status":"blocked","last_text":"正在跑全量","attention_reason":"本轮执行失败，再发一条消息继续","external_url":"https://applink.example.com/thread?x=1"}"#)
        #expect(s.listPreview == "本轮执行失败，再发一条消息继续")
        #expect(s.externalLink?.absoluteString == "https://applink.example.com/thread?x=1")
    }

    @Test func withoutAReasonTheLastWordsStay() throws {
        let s = try session(#"{"session_id":"s","status":"working","last_text":"在补测试","attention_reason":""}"#)
        #expect(s.listPreview == "在补测试")
        #expect(s.externalLink == nil)
        #expect(try session(#"{"session_id":"s","status":"idle"}"#).listPreview == nil)
    }

    @Test func onlyAWebLinkOpens() throws {
        for url in ["javascript:alert(1)", "lark://applink/x", "http://example.com", "not a url"] {
            #expect(try session(#"{"session_id":"s","status":"idle","external_url":"\#(url)"}"#).externalLink == nil, "\(url)")
        }
    }
}
