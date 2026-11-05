import Foundation
import Testing
@testable import Jarvis

/// LarkChatRows (#921): Feishu's messages and mine on their way in one list, mine where they were written.
struct LarkChatRowsTests {
    private let t0 = Date(timeIntervalSince1970: 1_800_000_000)

    private func message(_ id: String, at seconds: Double?, mine: Bool = false) -> LarkMessage {
        LarkMessage(id: id, position: id, senderName: mine ? "我" : "李雷", isMine: mine, type: "TEXT", text: id,
                    createdAt: seconds.map { t0.addingTimeInterval($0) }, recalled: false, replyCount: 0)
    }

    private func outgoing(_ text: String, at seconds: Double, failed: Bool = false) -> LarkOutgoing {
        var o = LarkOutgoing(text: text, at: t0.addingTimeInterval(seconds))
        if failed { o.state = .failed("超时") }
        return o
    }

    private func order(_ rows: [LarkChatRows.Row]) -> [String] {
        rows.map { $0.outgoing?.text ?? $0.message.id }
    }

    @Test func oneThatFailedStaysAboveWhatWentOutAfterIt() {
        let failed = outgoing("失败的", at: 5, failed: true)
        let rows = LarkChatRows.rows(messages: [message("m1", at: 0), message("后发成功的副本", at: 10, mine: true)],
                                     outgoing: [failed], like: nil)
        #expect(order(rows) == ["m1", "失败的", "后发成功的副本"])
        #expect(rows[1].outgoing == failed)
        #expect(rows[1].id == "outgoing-\(failed.id.uuidString)")
    }

    @Test func mineOnTheirWayGoUnderWhatCameBefore() {
        let rows = LarkChatRows.rows(messages: [message("m1", at: 0), message("m2", at: 3)],
                                     outgoing: [outgoing("发送中", at: 4)], like: nil)
        #expect(order(rows) == ["m1", "m2", "发送中"])
    }

    @Test func theyKeepTheOrderWrittenAroundAnAnswer() {
        let rows = LarkChatRows.rows(messages: [message("m1", at: 0), message("别人的回答", at: 15)],
                                     outgoing: [outgoing("一", at: 10, failed: true), outgoing("二", at: 20)], like: nil)
        #expect(order(rows) == ["m1", "一", "别人的回答", "二"])
    }

    @Test func aRetryKeepsItsPlace() {
        var o = outgoing("重发的", at: 5, failed: true)
        o.retry(at: t0.addingTimeInterval(30))
        #expect(o.at == t0.addingTimeInterval(30))
        #expect(o.written == t0.addingTimeInterval(5))
        o.retry(at: t0.addingTimeInterval(60))
        #expect(o.written == t0.addingTimeInterval(5), "a second 重发 does not move it either")
        let rows = LarkChatRows.rows(messages: [message("m1", at: 0), message("之后的", at: 20)], outgoing: [o], like: nil)
        #expect(order(rows) == ["m1", "重发的", "之后的"])
    }

    @Test func aMessageWithNoTimeStaysAbove() {
        let rows = LarkChatRows.rows(messages: [message("m1", at: 0), message("没有时间", at: nil)],
                                     outgoing: [outgoing("发送中", at: 4)], like: nil)
        #expect(order(rows) == ["m1", "没有时间", "发送中"])
    }

    @Test func mineAreDrawnLikeMyLastMessage() {
        let me = LarkMessage(id: "me", position: "1", senderID: "u-me", senderName: "小王", isMine: true, type: "TEXT", text: "",
                             createdAt: t0, recalled: false, replyCount: 0)
        let rows = LarkChatRows.rows(messages: [], outgoing: [outgoing("发送中", at: 4)], like: me)
        #expect(rows.first?.message.senderName == "小王")
        #expect(rows.first?.message.isMine == true)
    }
}
