import Foundation
import Testing
@testable import Persona

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

/// LarkChatRows with LarkOutbox.copies (#920): Feishu's copy of one of mine takes its row over under the same id, and
/// keeps it once the outgoing one is gone, so the list does not remove one row and insert another.
@MainActor
struct LarkChatRowsCopyTests {
    private let t0 = Date(timeIntervalSince1970: 1_800_000_000)

    private func copy(of o: LarkOutgoing, id: String = "om_copy", clientID: Bool = true) -> LarkMessage {
        LarkMessage(id: id, position: "9", senderName: "我", isMine: true, type: "TEXT", text: o.text,
                    createdAt: o.at.addingTimeInterval(1), recalled: false, replyCount: 0,
                    clientID: clientID ? o.clientID : nil)
    }

    private func others(_ id: String, at seconds: Double) -> LarkMessage {
        LarkMessage(id: id, position: id, senderName: "李雷", isMine: false, type: "TEXT", text: id,
                    createdAt: t0.addingTimeInterval(seconds), recalled: false, replyCount: 0)
    }

    @Test func theCopyTakesOverTheRowOfMineItAnswers() {
        let o = LarkOutgoing(text: "好的", at: t0.addingTimeInterval(5))
        let messages = [others("m1", at: 0), copy(of: o)]
        let copies = LarkOutbox.copies([o], messages: messages)
        #expect(copies == [o.id: "om_copy"])
        let rows = LarkChatRows.rows(messages: messages, outgoing: [o], like: nil, copies: copies)
        #expect(rows.map(\.id) == ["m1", o.rowID], "one row for it, under the outgoing row's id")
        #expect(rows[1].outgoing == nil && rows[1].message.id == "om_copy", "drawn as Feishu's message")
    }

    @Test func theRowKeepsItsIDOnceTheOutgoingOneIsGone() {
        var o = LarkOutgoing(text: "收到", at: t0.addingTimeInterval(5))
        o.state = .sent
        let messages = [others("m1", at: 0), copy(of: o, clientID: false)] // an older hub: matched by its words
        let copies = LarkOutbox.copies([o], messages: messages, now: o.at.addingTimeInterval(2))
        #expect(copies == [o.id: "om_copy"])
        let adopted = LarkChatRows.adopting(copies, into: [:])
        #expect(LarkOutbox.remaining([o], messages: messages, now: o.at.addingTimeInterval(2)).isEmpty)
        let rows = LarkChatRows.rows(messages: messages, outgoing: [], like: nil, adopted: adopted)
        #expect(rows.map(\.id) == ["m1", o.rowID])
    }

    @Test func aRowIDIsNeverDrawnTwice() {
        let o = LarkOutgoing(text: "还在发", at: t0.addingTimeInterval(5))
        // Kept from earlier, but the outgoing one is drawn again (its copy is not in this page).
        let rows = LarkChatRows.rows(messages: [others("m1", at: 0)], outgoing: [o], like: nil, adopted: ["m1": o.rowID])
        #expect(rows.map(\.id) == ["m1", o.rowID])
    }

    /// An encrypted one has no words Feishu shows: like before #920, it waits for its exact copy (client ID or receipt)
    /// and is not let go after `copyWait`.
    @Test func anEncryptedOneIsNotMatchedByItsWords() {
        var o = LarkOutgoing(text: "密", at: t0)
        o.encrypted = LarkEncryptedPayload(accountId: "a", feedId: "f", messages: [.init(text: "密")], cover: nil)
        o.state = .sent
        let messages = [copy(of: o, clientID: false)]
        #expect(LarkOutbox.copies([o], messages: messages, now: t0.addingTimeInterval(2)).isEmpty)
        #expect(LarkOutbox.remaining([o], messages: messages, now: t0.addingTimeInterval(LarkOutbox.copyWait + 1)).map(\.id) == [o.id])
    }

    @Test func remainingIsWhatHasNoCopy() {
        let a = LarkOutgoing(text: "一", at: t0), b = LarkOutgoing(text: "二", at: t0)
        let messages = [copy(of: a)]
        #expect(LarkOutbox.copies([a, b], messages: messages) == [a.id: "om_copy"])
        #expect(LarkOutbox.remaining([a, b], messages: messages).map(\.id) == [b.id])
    }
}
