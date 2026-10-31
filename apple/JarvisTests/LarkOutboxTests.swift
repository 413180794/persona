import Foundation
import Testing
@testable import Jarvis

@MainActor
struct LarkOutboxTests {
    private func mine(_ text: String, at: Date) -> LarkMessage {
        LarkMessage(id: UUID().uuidString, position: "1", senderName: "我", isMine: true, type: "TEXT", text: text,
                    createdAt: at, recalled: false, replyCount: 0)
    }

    @Test func aSentMessageGivesWayToItsCopy() {
        let now = Date()
        var o = LarkOutgoing(text: "好的", at: now)
        o.state = .sent
        #expect(LarkOutbox.remaining([o], messages: [], now: now).count == 1)
        #expect(LarkOutbox.remaining([o], messages: [mine(" 好的\n", at: now.addingTimeInterval(1))], now: now).isEmpty)
    }

    @Test func sendingAndFailedOnesStay() {
        let now = Date()
        let sending = LarkOutgoing(text: "好的", at: now)
        var failed = LarkOutgoing(text: "好的", at: now)
        failed.state = .failed("超时")
        let left = LarkOutbox.remaining([sending, failed], messages: [mine("好的", at: now)], now: now)
        #expect(left.count == 2)
    }

    @Test func oneCopyAnswersOneMessage() {
        let now = Date()
        var a = LarkOutgoing(text: "收到", at: now)
        var b = LarkOutgoing(text: "收到", at: now)
        a.state = .sent
        b.state = .sent
        let left = LarkOutbox.remaining([a, b], messages: [mine("收到", at: now)], now: now)
        #expect(left.map(\.id) == [b.id])
    }

    @Test func anOldMessageWithTheSameWordsIsNotItsCopy() {
        let now = Date()
        var o = LarkOutgoing(text: "好的", at: now)
        o.state = .sent
        #expect(LarkOutbox.remaining([o], messages: [mine("好的", at: now.addingTimeInterval(-3600))], now: now).count == 1)
    }

    @Test func aSentMessageGoesAfterAWhileAnyway() {
        let now = Date()
        var o = LarkOutgoing(text: "好的", at: now.addingTimeInterval(-LarkOutbox.copyWait - 1))
        o.state = .sent
        #expect(LarkOutbox.remaining([o], messages: [], now: now).isEmpty)
    }
}
