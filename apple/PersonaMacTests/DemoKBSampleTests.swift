import Foundation
import Testing
@testable import Persona

/// The knowledge-search sample the demo answers with for screenshots (`-showKnowledgeSearch`, `-showKnowledgeDay`):
/// nothing without the flags, and with them answers the app's own types decode, so the search hits, an entry's
/// matches and a chat's day all have something to show.
@MainActor struct DemoKBSampleTests {
    private func decode<T: Decodable>(_ v: JSONValue?, as: T.Type) throws -> T {
        try JSON.decoder.decode(T.self, from: try JSON.encoder.encode(try #require(v)))
    }

    @Test func nothingWithoutTheFlags() {
        #expect(DemoKBSample.answer(["kb-search"], ["q": "小组件"], kb: nil, asked: false) == nil)
        #expect(DemoKBSample.answer(["kb", "chats", DemoKBSample.chatID], [:], kb: nil, asked: false) == nil)
    }

    @Test func searchFindsEntriesOfThreeKinds() throws {
        struct R: Decodable { var results: [KBResult] }
        // The demo's own entries (Demo.json's kb) are the project and the person found.
        let kb: JSONValue = [
            "projects": [["kind": "projects", "id": "weather", "name": "天气 App", "relevance": 90]],
            "people": [["kind": "people", "id": "han-meimei", "name": "韩梅梅", "relevance": 70]],
        ]
        let r = try decode(DemoKBSample.answer(["kb-search"], ["q": "小组件", "results": "1"], kb: kb, asked: true), as: R.self)
        #expect(r.results.map(\.entry.id) == ["weather", DemoKBSample.chatID, "han-meimei"])
        let chat = try #require(r.results.first { $0.entry.kind == "chats" })
        #expect(chat.entry.name == DemoKBSample.chatName)
        #expect(chat.hits?.allSatisfy { $0.day != nil } == true)
    }

    @Test func theChatOpenedFromASearchHasMatchesOnDays() throws {
        let d = try decode(DemoKBSample.answer(["kb", "chats", DemoKBSample.chatID], ["q": "小组件"], kb: nil, asked: true),
                           as: KBDetailResponse.self)
        #expect(d.detail.meta?["name"]?.string == DemoKBSample.chatName)
        #expect((d.matches ?? []).count == 3)
        #expect(d.matches?.allSatisfy { $0.day != nil } == true)
        // Opened without a search: no matches.
        let plain = try decode(DemoKBSample.answer(["kb", "chats", DemoKBSample.chatID], [:], kb: nil, asked: true),
                               as: KBDetailResponse.self)
        #expect(plain.matches == nil)
    }

    @Test func aDayFoldsContinuationLinesAndLinksTheDayBefore() throws {
        let now = Date(timeIntervalSince1970: 1_790_000_000)
        let today = DemoKBSample.day(now, 0), earlier = DemoKBSample.day(now, -2)
        let page = try decode(DemoKBSample.answer(["kb", "chats", DemoKBSample.chatID, "days", today], [:], kb: nil, asked: true, now: now),
                              as: KBDay.self)
        #expect(page.messages.count == 8)
        // The found message (line 4 of the day file) carries its continuation line (line 5).
        #expect(page.messages.first { $0.n == 4 }?.text.contains("\n三张") == true)
        #expect(!page.messages.contains { $0.n == 5 })
        #expect(page.prev == earlier)
        #expect(page.next == "")
        let before = try decode(DemoKBSample.answer(["kb", "chats", DemoKBSample.chatID, "days", earlier], [:], kb: nil, asked: true, now: now),
                                as: KBDay.self)
        #expect(before.next == today)
    }
}
