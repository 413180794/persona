import Foundation
import Testing
@testable import Persona

/// Quotes as Telegram's replies (#986): a quoted message carries its id through the `<persona-context>` tag, so the
/// sent bubble leads back to it; what the user picked shows as quotes, what the hub put in a Feishu /agent request folds.
struct QuoteTests {
    @Test func aQuotedMessageKeepsItsIDThroughTheTag() {
        let ref = ContextRef.quote("PR #42 看完了：逻辑没问题", from: "Claude", msg: "item-\"7\"")
        let parsed = ContextRef.parse(ref.xml + "\n\n按这个改")
        #expect(parsed.rest == "按这个改")
        #expect(parsed.refs.count == 1)
        #expect(parsed.refs[0].kind == "quote")
        #expect(parsed.refs[0].msg == "item-\"7\"")
        #expect(parsed.refs[0].text == "PR #42 看完了：逻辑没问题")
    }

    @Test func aTagWithoutTheIDReadsAsBefore() {
        let ref = ContextRef(kind: "context", title: "页面", url: "https://example.com/a", text: "选中的文字")
        #expect(!ref.xml.contains("msg="))
        #expect(ContextRef.parse(ref.xml).refs.first?.msg == "")
    }

    @Test func headingsSayWhatIsQuoted() {
        #expect(ContextRef.quote("x", from: "我", msg: "1").quoteHeading == "引用 你的消息")
        #expect(ContextRef.quote("x", from: "Codex", msg: "1").quoteHeading == "引用 Codex")
        let page = ContextRef(kind: "context", title: "时间线", url: "https://example.com/t", text: "一段")
        #expect(page.quoteHeading == "引用选中的文字 · 时间线")
        let whole = ContextRef(kind: "context", title: "时间线", url: "https://example.com/t", text: "（整篇）")
        #expect(whole.quoteHeading == "引用 时间线")
        #expect(whole.quoteSummary == "example.com")
        let session = ContextRef(kind: "context", title: "照片同步", url: "persona://session/m/s",
                                 text: "另一个会话：照片同步（Claude）\n目录：~/code/photos")
        #expect(session.quoteHeading == "引用会话 照片同步")
        #expect(session.quoteSummary == "目录：~/code/photos")
    }

    @Test func theHubsContextFoldsAndPicksAreQuotes() {
        let picks = [ContextRef.quote("x", from: "Claude", msg: "1"),
                     ContextRef(kind: "context", title: "录音", url: "", text: "转写")]
        #expect(ContextRef.quotes(picks)?.count == 2)
        let agent = picks + [ContextRef(kind: "instructions", title: "给 Agent 的说明", url: "", text: "…")]
        #expect(ContextRef.quotes(agent) == nil)
        #expect(ContextRef.quotes([]) == nil)
    }
}
