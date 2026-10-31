import Foundation
import Testing
@testable import Jarvis

/// A 飞书 chat's paging and read reporting (`LarkPager`), without the chat.
@MainActor struct LarkPagerTests {
    private func msg(_ p: Int, mine: Bool = false, read: Bool? = nil, text: String = "") -> LarkMessage {
        let json = """
        {"id":"m\(p)","position":"\(p)","sender_name":"\(mine ? "我" : "韩梅梅")","is_mine":\(mine),"type":"TEXT","text":"\(text)",
         "recalled":false,"reply_count":0\(read.map { ",\"is_read\":\($0)" } ?? "")}
        """
        return try! JSON.decoder.decode(LarkMessage.self, from: Data(json.utf8))
    }

    private func page(_ ps: [Int], older: Bool = false, newer: Bool? = nil, olderCursor: String? = nil, newerCursor: String? = nil) -> APIClient.LarkMessages {
        APIClient.LarkMessages(messages: ps.map { msg($0) }, hasOlder: older, hasNewer: newer, olderCursor: olderCursor, newerCursor: newerCursor)
    }

    @Test func aPageSetsBothCursors() {
        var p = LarkPager()
        p.replace(with: page([5, 6, 7], older: true, newer: true))
        #expect(p.olderRequest == "5" && p.newerRequest == "7")
        #expect(p.hasOlder && p.hasNewer)
        p.replace(with: page([5, 6, 7], olderCursor: "4x", newerCursor: "8x"))
        #expect(p.olderRequest == "4x" && p.newerRequest == "8x", "the hub's cursors win over positions")
    }

    @Test func olderAndNewerPagesJoinWithoutRepeats() {
        var p = LarkPager()
        p.replace(with: page([5, 6], older: true, newer: true))
        p.prepend(page([3, 4, 5], older: false))
        #expect(p.messages.map(\.position) == ["3", "4", "5", "6"])
        #expect(!p.hasOlder)
        p.append(page([6, 7], newer: false), from: "6")
        #expect(p.messages.map(\.position) == ["3", "4", "5", "6", "7"])
        #expect(!p.hasNewer && p.newerRequest == "7")
        p.append(page([], newer: false), from: "7")
        #expect(p.newerRequest == "7", "an empty page keeps where it asked from")
    }

    @Test func aPollUpdatesInPlaceAndReturnsOnlyWhatIsNewer() {
        var p = LarkPager()
        p.replace(with: page([1, 2]))
        var latest = page([2, 3])
        latest.messages[0].text = "edited"
        let fresh = p.refresh(with: latest)
        #expect(p.messages.map(\.text) == ["", "edited"])
        #expect(fresh.map(\.position) == ["3"])
        p.add(fresh)
        #expect(p.newerRequest == "3")
    }

    @Test func twoPollsAtOnceAddANewMessageOnce() {
        var p = LarkPager()
        p.replace(with: page([1, 2]))
        let a = p.refresh(with: page([2, 3])), b = p.refresh(with: page([2, 3])) // both before either adds
        p.add(a)
        p.add(b)
        #expect(p.messages.map(\.position) == ["1", "2", "3"])
    }

    @Test func readIsReportedUpToTheNewestSeenWithOthersUnreadIDs() {
        var p = LarkPager()
        p.messages = [msg(1), msg(2, mine: true), msg(3), msg(4, read: true), msg(5)]
        p.readCursor = "1"
        let none = p.readReport()
        #expect(none == nil, "nothing seen yet")
        let first = p.see(p.messages[3]), again = p.see(p.messages[3])
        #expect(first && !again, "seen once")
        let r = p.readReport()
        #expect(r?.position == "4")
        #expect(r?.messageIDs == ["m3"], "not mine, not already read, past the cursor, up to the newest seen")
        #expect(p.readCursor == "4")
        let after = p.readReport()
        #expect(after == nil, "nothing new since")
    }
}
