import Foundation
import Testing
@testable import Persona

/// Read receipts of my Lark messages: what a receipt shows (`LarkReadMark`), which ones are asked for and when
/// (`LarkReadReceipts`), and the request (`larkReadStates`).
@MainActor struct LarkReadReceiptsTests {
    private func state(_ id: String = "m1", read: Int, unread: Int) -> LarkReadState {
        LarkReadState(messageID: id, readCount: read, unreadCount: unread)
    }

    private func msg(_ id: String, mine: Bool = true, type: String = "TEXT", recalled: Bool = false) -> LarkMessage {
        LarkMessage(id: id, position: id, senderName: mine ? "我" : "Tom", isMine: mine, type: type, text: "hi", recalled: recalled, replyCount: 0)
    }

    // MARK: What shows

    @Test func aOneToOneChatSaysReadOrUnread() {
        #expect(LarkReadMark(state: state(read: 1, unread: 0), isGroup: false) == .read)
        #expect(LarkReadMark(state: state(read: 0, unread: 1), isGroup: false) == .unread)
    }

    @Test func aGroupFillsARingAndTicksWhenAllHaveRead() {
        #expect(LarkReadMark(state: state(read: 5, unread: 0), isGroup: true) == .allRead)
        let some = LarkReadMark(state: state(read: 3, unread: 2), isGroup: true)
        #expect(some == .progress(read: 3, total: 5))
        #expect(some?.fraction == 0.6)
        #expect(some?.label == "2 人未读")
        #expect(LarkReadMark(state: state(read: 0, unread: 5), isGroup: true) == .progress(read: 0, total: 5), "nobody yet: an empty ring")
    }

    @Test func noReceiptShowsNothing() {
        #expect(LarkReadMark(state: nil, isGroup: false) == nil, "never claim read without a receipt")
        #expect(LarkReadMark(state: nil, isGroup: true) == nil)
        #expect(LarkReadMark(state: state(read: 0, unread: 0), isGroup: true) == nil, "nobody to read it")
        #expect(LarkReadMark(state: state(read: -1, unread: 2), isGroup: true) == nil)
    }

    // MARK: What is asked for, and when

    @Test func onlyMineInFeishuAreAskedFor() {
        var r = LarkReadReceipts()
        for m in [msg("m1"), msg("m2", mine: false), msg("m3", type: "SYSTEM"), msg("m4", recalled: true), msg("outgoing-x")] {
            r.appeared(m)
        }
        #expect(r.take(now: .now) == ["m1"])
    }

    @Test func requestsAreThrottledPerChat() {
        let t0 = Date(timeIntervalSince1970: 1_000_000)
        var r = LarkReadReceipts()
        #expect(r.wait(now: t0) == nil, "nothing to ask")
        r.appeared(msg("m1"))
        #expect(r.wait(now: t0) == 0, "the first goes at once")
        _ = r.take(now: t0)
        r.appeared(msg("m2"))
        #expect(r.wait(now: t0.addingTimeInterval(1)) == LarkReadReceipts.minGap - 1)
        #expect(r.wait(now: t0.addingTimeInterval(LarkReadReceipts.minGap)) == 0)
    }

    @Test func oneRequestCarriesAtMostAHundred() {
        var r = LarkReadReceipts()
        for i in 0..<150 { r.appeared(msg(String(format: "m%03d", i))) }
        let first = r.take(now: .now)
        #expect(first.count == LarkReadReceipts.batchLimit)
        #expect(r.take(now: .now).count == 50, "the rest go next time")
    }

    @Test func aMessageEveryoneReadIsNotAskedAgain() {
        var r = LarkReadReceipts()
        r.appeared(msg("m1"))
        r.appeared(msg("m2"))
        _ = r.take(now: .now)
        r.record([state("m1", read: 5, unread: 0), state("m2", read: 3, unread: 2)])
        r.chatChanged()
        #expect(r.take(now: .now) == ["m2"], "only the one some have not read yet")
        r.disappeared("m2")
        r.appeared(msg("m1")) // back on screen
        #expect(r.wait(now: .distantFuture) == nil, "m1 is final, m2 is off screen")
    }

    @Test func offScreenIsNotAskedAbout() {
        var r = LarkReadReceipts()
        r.appeared(msg("m1"))
        r.disappeared("m1")
        r.chatChanged()
        #expect(r.wait(now: .now) == nil)
    }

    @Test func anOldHubIsNotAskedAgainButOtherFailuresWaitForAChange() {
        var r = LarkReadReceipts()
        r.appeared(msg("m1"))
        _ = r.take(now: .now)
        r.failed(status: 502)
        #expect(r.wait(now: .distantFuture) == nil, "no retry loop")
        r.chatChanged()
        #expect(r.wait(now: .distantFuture) == 0, "a change asks again")
        _ = r.take(now: .now)
        r.failed(status: 404)
        r.chatChanged()
        r.appeared(msg("m2"))
        #expect(r.wait(now: .distantFuture) == nil, "the hub has no route: not asked again")
    }

    // MARK: The request

    @Test func theRequestCarriesTheIDsAndReadsTheStates() async throws {
        let hub = FakeHub()
        hub.on("POST", "/api/v1/lark/conversations/f1/read-states",
               json: #"{"states":[{"message_id":"m91","read_count":1,"unread_count":0}]}"#)
        let got = try await hub.client.larkReadStates("f1", ids: ["m91", "m104"])
        #expect(got == [state("m91", read: 1, unread: 0)], "a message without a receipt is simply missing")
        let body = try JSONSerialization.jsonObject(with: hub.sent("POST", "/api/v1/lark/conversations/f1/read-states")[0].body ?? Data())
        #expect((body as? NSDictionary) == ["message_ids": ["m91", "m104"]])
    }

    @Test func anOldHubAnswers404() async {
        let hub = FakeHub() // no read-states route: 404
        do {
            _ = try await hub.client.larkReadStates("f1", ids: ["m1"])
            Issue.record("expected an error")
        } catch {
            #expect((error as? APIError)?.status == 404)
        }
    }
}
