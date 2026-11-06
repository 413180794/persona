import Foundation
import Testing
@testable import Persona

struct LarkChatStatesTests {
    private func hub(_ status: Int, _ message: String, code: String? = nil) -> APIError {
        APIError(status: status, message: message, fields: code.map { ["code": $0] } ?? [:])
    }

    @Test func theHubsCodeSaysTheGatewayIsLoggedOut() {
        let p = LarkLoadProblem.of(hub(502, "飞书网关未登录飞书", code: "lark_not_logged_in"))
        #expect(p.kind == .notLoggedIn)
        #expect(p.title == "飞书登录已失效")
        #expect(p.message.contains("hub 所在机器"))
        #expect(p.symbol == Theme.Icon.credentials)
        #expect(p.inline?.contains("重新登录飞书网关") == true)
    }

    @Test func aHubWithoutTheCodeIsReadByItsWords() {
        #expect(LarkLoadProblem.of(hub(502, "飞书网关未登录飞书")).kind == .notLoggedIn)
        // Only the Lark gateway's 502 says it; the same words elsewhere are another error.
        #expect(LarkLoadProblem.of(hub(500, "飞书网关未登录飞书")).kind == .other)
        // The code alone is enough, whatever the words.
        #expect(LarkLoadProblem.of(hub(502, "session expired", code: "lark_not_logged_in")).kind == .notLoggedIn)
    }

    @Test func otherHubErrorsShowTheirWordsAndAnInlineLine() {
        let e = hub(502, "lark: upstream said no")
        let p = LarkLoadProblem.of(e)
        #expect(p.kind == .other)
        #expect(p.title == "没能加载这个会话")
        #expect(p.message == e.localizedDescription)
        #expect(p.inline == e.localizedDescription)
        #expect(p.symbol == Theme.Icon.error)
    }

    @Test func theThreadSheetSaysWhatDidNotLoad() {
        let e = hub(502, "lark: upstream said no")
        let thread = LarkLoadProblem.of(e, subject: "这个话题")
        #expect(thread.title == "没能加载这个话题")
        #expect(thread.message == e.localizedDescription)
        #expect(LarkLoadProblem.of(e).title == "没能加载这个会话", "the chat page keeps its words")
        // Everything else reads the same on both pages: the login hint, no network.
        let loggedOut = hub(502, "飞书网关未登录飞书", code: "lark_not_logged_in")
        #expect(LarkLoadProblem.of(loggedOut, subject: "这个话题") == LarkLoadProblem.of(loggedOut))
        #expect(LarkLoadProblem.of(URLError(.cannotConnectToHost), subject: "这个话题") == LarkLoadProblem.of(URLError(.cannotConnectToHost)))
    }

    @Test func noNetworkAndTimeoutsStayQuietOverMessages() {
        let offline = LarkLoadProblem.of(URLError(.cannotConnectToHost))
        #expect(offline.kind == .offline)
        #expect(offline.title == "连不上 hub")
        #expect(offline.inline == nil)
        let late = LarkLoadProblem.of(URLError(.timedOut))
        #expect(late.kind == .timeout)
        #expect(late.inline == nil)
        #expect(LarkLoadProblem.of(hub(504, "gateway timeout")).kind == .timeout)
    }

    @Test func thePlaceholderOnlyStandsInForNothing() {
        let p = LarkLoadProblem.of(hub(502, "飞书网关未登录飞书"))
        #expect(LarkChatPlaceholder.of(hasContent: true, loading: true, problem: p) == nil)
        #expect(LarkChatPlaceholder.of(hasContent: false, loading: true, problem: nil) == .loading)
        #expect(LarkChatPlaceholder.of(hasContent: false, loading: false, problem: nil) == .empty)
        #expect(LarkChatPlaceholder.of(hasContent: false, loading: false, problem: p) == .failed(p))
        // A retry clears the problem and loads again: the spinner, not the old error.
        #expect(LarkChatPlaceholder.of(hasContent: false, loading: true, problem: nil) == .loading)
    }
}
