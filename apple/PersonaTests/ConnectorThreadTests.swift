import Foundation
import Testing
@testable import Jarvis

/// A connector's inbox thread as a conversation page (#1054): pages of messages going back, a quote's message reached,
/// a reply through the hub (which asks on the phone), what the page shows when the thread cannot be read. Made-up
/// people and words only.
@MainActor
struct ConnectorThreadTests {
    private func message(_ id: Int, from: String = "u1", mine: Bool = false, at minute: Int = 0, text: String = "示例消息") -> String {
        let name = mine ? "我" : (from == "u1" ? "示例用户" : "另一个示例用户")
        return #"{"id":"\#(id)","at":"2026-11-03T09:\#(String(format: "%02d", minute)):00Z","sender":{"id":"\#(mine ? "" : from)","name":"\#(name)"},"mine":\#(mine),"text":"\#(text) \#(id)"}"#
    }

    /// Pages of 3, newest first: 7–9, then 4–6, then 1–3 (the start).
    private func hub() -> FakeHub {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/connectors/telegram/thread") { req in
            let end = Int(req.query["before"] ?? "10") ?? 10
            let ids = Array(max(end - 3, 1)..<end)
            let msgs = ids.map { self.message($0, at: $0) }.joined(separator: ",")
            let before = ids.first.map { $0 > 1 ? #","before":"\#($0)""# : "" } ?? ""
            return Data(#"{"chat":{"id":"u1","name":"示例用户","kind":"dm","reply":"telegram_send"},"messages":[\#(msgs)]\#(before)}"#.utf8)
        }
        return hub
    }

    @Test func pagesGoBack() async throws {
        let hub = hub()
        let m = ConnectorThreadModel(connector: "telegram", thread: "u1", api: { hub.client })
        await m.load()
        #expect(m.chat?.name == "示例用户")
        #expect(m.chat?.isChat == true)
        #expect(m.messages.map(\.id) == ["7", "8", "9"])
        #expect(m.hasOlder)
        #expect(await m.loadOlder())
        #expect(m.messages.map(\.id) == ["4", "5", "6", "7", "8", "9"])
        // A quote's message further back: read back until it is there.
        #expect(await m.reach("1"))
        #expect(m.messages.first?.id == "1")
        #expect(!m.hasOlder)
        #expect(await !m.loadOlder())
        #expect(await !m.reach("0"))
        #expect(hub.sent("GET", "/api/v1/connectors/telegram/thread").map { $0.query["thread"] } == ["u1", "u1", "u1"])
    }

    /// The reply goes to the hub with the thread's action and the message it answers, then the newest page is read again.
    @Test func replyThroughTheHub() async throws {
        let hub = hub()
        hub.on("POST", "/api/v1/connectors/telegram/thread/reply", json: #"{"ok":true}"#)
        let m = ConnectorThreadModel(connector: "telegram", thread: "u1", api: { hub.client })
        await m.load()
        let answered = try #require(m.messages.last)
        #expect(await m.reply("好的", to: answered))
        let body = try #require(hub.sent("POST", "/api/v1/connectors/telegram/thread/reply").first?.body)
        let sent = try JSONSerialization.jsonObject(with: body) as? [String: String]
        #expect(sent == ["thread": "u1", "action": "telegram_send", "text": "好的", "reply_to": "9"])
        #expect(hub.sent("GET", "/api/v1/connectors/telegram/thread").count == 2)

        hub.on("POST", "/api/v1/connectors/telegram/thread/reply") { _ in throw APIError(status: 403, message: "The user declined telegram_send.") }
        #expect(await !m.reply("算了", to: nil))
        #expect(m.failure?.contains("没有发送") == true)
    }

    /// A thread the hub cannot read (no conversations for that connector, signed out) leaves the page to what the hub
    /// collected; a rate limit says how long to wait.
    @Test func unreadable() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/connectors/github/thread") { _ in throw APIError(status: 409, message: "GitHub has no conversations to open") }
        let gh = ConnectorThreadModel(connector: "github", thread: "example-org/example-repo#12", api: { hub.client })
        await gh.load()
        #expect(gh.loaded && gh.unavailable)
        hub.on("GET", "/api/v1/connectors/telegram/thread") { _ in throw APIError(status: 429, message: "Telegram 限流：还要 248 秒") }
        let tg = ConnectorThreadModel(connector: "telegram", thread: "u1", api: { hub.client })
        await tg.load()
        #expect(tg.unavailable)
        #expect(tg.failure == "Telegram 限流：还要 248 秒")
    }

    /// The fields of a page decode as the hub sends them: media, a quote, an event.
    @Test func decodes() throws {
        let raw = #"""
        {"chat":{"id":"c2","name":"示例群","kind":"group","avatar":"a.c2.900","members":12},
         "messages":[{"id":"1","at":"2026-11-03T08:30:00Z","sender":{"name":"示例群"},"event":true,"text":"示例用户 进群"},
                     {"id":"9","at":"2026-11-03T09:30:00Z","sender":{"id":"u8","name":"第三个示例用户","avatar":"a.u8.508"},
                      "media":[{"kind":"video","thumb":"m.c2.9.t","duration":65,"width":1280,"height":720}],
                      "reply_to":{"id":"8","from":"第三个示例用户","text":"演示"}}],
         "before":"1"}
        """#
        let page = try JSON.decoder.decode(ThreadPage.self, from: Data(raw.utf8))
        #expect(page.chat.isGroup && page.chat.isChat && page.chat.reply == nil)
        #expect(page.messages[0].isEvent)
        let video = try #require(page.messages[1].media?.first)
        #expect(video.picture == "m.c2.9.t")
        #expect(page.messages[1].replyTo?.from == "第三个示例用户")
        #expect(page.messages[1].summary == "[视频 1:05]")
        #expect(ThreadMediaText.duration(3723) == "1:02:03")
        #expect(ConnectorMedia.url("telegram", ref: "a.u1.501") == "/api/v1/connectors/telegram/media?ref=a.u1.501")
        let pr = try JSON.decoder.decode(ThreadChat.self, from: Data(#"{"id":"example-org/example-repo#12","name":"示例 PR","kind":"pr"}"#.utf8))
        #expect(!pr.isChat)
    }

    /// Like the Feishu chat: a time above a new day or a pause; one avatar and name for a run from the same person,
    /// mine apart from theirs, an event apart from both.
    @Test func runsAndSeparators() throws {
        func decode(_ json: String) throws -> ThreadMessage { try JSON.decoder.decode(ThreadMessage.self, from: Data(json.utf8)) }
        let a = try decode(message(1, at: 0)), b = try decode(message(2, at: 1)), c = try decode(message(3, from: "u7", at: 2))
        let mine = try decode(message(4, mine: true, at: 3)), late = try decode(message(5, at: 30))
        #expect(ThreadLayout.separates(a, after: nil))
        #expect(!ThreadLayout.separates(b, after: a))
        #expect(ThreadLayout.continues(b, after: a))
        #expect(!ThreadLayout.continues(c, after: b))
        #expect(!ThreadLayout.continues(mine, after: c))
        #expect(ThreadLayout.separates(late, after: mine))
        #expect(!ThreadLayout.continues(late, after: mine))
    }
}
