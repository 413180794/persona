import Foundation
import Testing
@testable import Jarvis

/// A message sent from the phone shows as a pending bubble until its copy in the transcript arrives (ChatStore.merge).
@MainActor
struct PendingArrivalTests {
    private func user(_ seq: Int, _ text: String, at: Date?, fullSize: Int? = nil) -> Item {
        var it = Item(id: "u\(seq)#0", seq: seq, ts: at, role: "user", kind: .text, text: text, toolUseID: nil, toolName: nil,
                      toolInput: nil, isError: nil, image: nil)
        it.fullSize = fullSize
        return it
    }

    private func store() -> ChatStore { ChatStore(key: SessionKey(machineID: "box", sessionID: "s1")) }

    // A long message comes back cut for the page (it ends in the 已截断 line): it is still the message, and the bubble
    // went away only after a minute, the message shown twice until then (2026-10-31, a 16 KB paste).
    @Test func aLongMessageCutByThePageArrives() {
        let s = store()
        let long = (0..<500).map { String(format: "%05d 编造的一行", $0) }.joined(separator: "\n")
        s.pending = [PendingMessage(text: long, images: [])]
        let cut = String(long.prefix(4000)) + "\n…（已截断，点开看全文，共 31561 字节）"
        s.merge([user(5, cut, at: Date(), fullSize: 31561)])
        #expect(s.pending.isEmpty)
    }

    // An older 「继续」 in a history page is not the 「继续」 just written: the unsent bubble used to vanish with it.
    @Test func anOlderMessageWithTheSameWordsIsNotIt() {
        let s = store()
        var p = PendingMessage(text: "继续", images: [])
        p.state = .waiting
        s.pending = [p]
        s.merge([user(3, "继续", at: Date().addingTimeInterval(-3600))], backlog: true)
        #expect(s.pending.count == 1)
        s.merge([user(9, "继续", at: Date())])
        #expect(s.pending.isEmpty)
    }

    // A batch that brings something new along with a message already shown does not match the old one again.
    @Test func onlyNewItemsCount() {
        let s = store()
        s.merge([user(3, "继续", at: nil)])
        s.pending = [PendingMessage(text: "继续", images: [])]
        s.merge([user(3, "继续", at: nil), user(4, "别的", at: nil)])
        #expect(s.pending.count == 1)
    }

    // Two 「好」 sent one after the other: the first copy takes one bubble.
    @Test func oneCopyTakesOneBubble() {
        let s = store()
        s.pending = [PendingMessage(text: "好", images: []), PendingMessage(text: "好", images: [])]
        s.merge([user(7, "好", at: Date())])
        #expect(s.pending.count == 1)
    }

    // Outboxes saved before createdAt was kept still load.
    @Test func oldOutboxesDecode() throws {
        let old = #"{"id":"6F9619FF-8B86-D011-B42D-00C04FC964FF","text":"好","images":[],"state":{"waiting":{}}}"#
        let p = try JSONDecoder().decode(PendingMessage.self, from: Data(old.utf8))
        #expect(p.createdAt == nil && p.state == .waiting)
        #expect(PendingMessage.arrived(p, as: user(1, "好", at: Date(timeIntervalSince1970: 0))))
    }
}
