import Foundation
import Testing
@testable import Persona

/// Commenting in the Feishu document reader (#1290): where Send goes, what it sends (the @s the text still holds, the
/// passage it quotes), a failed send keeping the words and retrying with the same id, resolving and reopening, and
/// only a hub whose plugin says `lark_doc_comments` offering it.
@MainActor
struct FeishuDocsCommentsTests {
    /// Records what the model asks for; `fail` makes the next writes throw.
    final class FakeAPI: FeishuDocsCommentsAPI, @unchecked Sendable {
        var threads: [FeishuDocsComments.Thread]
        var writes: [(route: String, write: FeishuDocsCommentWrite?)] = []
        var resolves: [(thread: String, resolved: Bool)] = []
        var reads = 0
        var fail = false

        init(_ threads: [FeishuDocsComments.Thread]) { self.threads = threads }

        func docComments(url: String) async throws -> FeishuDocsComments {
            reads += 1
            return FeishuDocsComments(threads: threads)
        }
        func docComment(_ c: FeishuDocsCommentWrite) async throws {
            writes.append(("comment", c))
            if fail { throw APIError(status: 502, message: "飞书网关未登录飞书") }
        }
        func docReply(thread: String, _ c: FeishuDocsCommentWrite) async throws {
            writes.append(("reply:" + thread, c))
            if fail { throw APIError(status: 502, message: "飞书网关未登录飞书") }
        }
        func docResolve(thread: String, url: String, resolved: Bool) async throws {
            resolves.append((thread, resolved))
            if fail { throw APIError(status: 502, message: "飞书网关未登录飞书") }
            if let i = threads.firstIndex(where: { $0.id == thread }) { threads[i].resolved = resolved }
        }
        func docPeople(url: String, query: String) async throws -> [LarkMember] {
            [LarkMember(userID: "ou_1", name: "李雷")].filter { query.isEmpty || $0.name.contains(query) }
        }
    }

    private func thread(_ id: String, resolved: Bool = false, by name: String = "韩梅梅") -> FeishuDocsComments.Thread {
        FeishuDocsComments.Thread(id: id, whole: false, resolved: resolved, quote: "这一段", anchorBlockId: nil, createdAt: nil,
                                  replies: [FeishuDocsComments.Reply(id: id + "-r", author: FeishuDocsPerson(id: nil, name: name, avatarUrl: nil),
                                                                     content: [.init(kind: "text", text: "圆角统一成多少？", href: nil)])],
                                  visibility: nil)
    }

    @Test func aCommentOnTheDocumentWithItsMentions() async {
        let api = FakeAPI([thread("c1")])
        let m = FeishuDocsCommentsModel(url: "https://example.feishu.cn/docx/a", api: api)
        await m.load()
        m.draft = "  @李雷 看一下  "
        m.mentions = [LarkMention(userID: "ou_1", name: "李雷"), LarkMention(userID: "ou_2", name: "王五")] // 「王五」 was deleted again
        await m.send()
        #expect(api.writes.count == 1 && api.writes[0].route == "comment")
        let w = api.writes[0].write!
        #expect(w.content == "@李雷 看一下")
        #expect(w.mentions == [LarkMention(userID: "ou_1", name: "李雷")])
        #expect(w.quote == nil && w.url == "https://example.feishu.cn/docx/a")
        #expect(m.draft.isEmpty && m.mentions.isEmpty && m.target == .document)
        #expect(api.reads == 2) // read again after the write
    }

    @Test func aReplyGoesToItsThreadThenTheBoxCommentsOnTheDocumentAgain() async {
        let api = FakeAPI([thread("c1"), thread("c2", by: "王五")])
        let m = FeishuDocsCommentsModel(url: "u", api: api)
        await m.load()
        m.reply(to: m.threads![1])
        #expect(m.target == .thread(id: "c2", name: "王五", text: "圆角统一成多少？"))
        m.draft = "好"
        await m.send()
        #expect(api.writes.map(\.route) == ["reply:c2"])
        #expect(m.target == .document)
    }

    @Test func aPassageCommentQuotesItsText() async {
        let api = FakeAPI([])
        let m = FeishuDocsCommentsModel(url: "u", api: api, target: .passage(quote: "统一了圆角", blockID: "b2"))
        m.draft = "改成 12"
        await m.send()
        #expect(api.writes.first?.write?.quote == "统一了圆角")
        #expect(api.writes.first?.write?.blockID == "b2")
    }

    /// A send that fails keeps the words and where they were going, says why, and trying again is the same send (the
    /// plugin's receipt drops a second copy); other words are a new one.
    @Test func aFailedSendKeepsTheDraftAndRetriesWithTheSameID() async {
        let api = FakeAPI([thread("c1")])
        let m = FeishuDocsCommentsModel(url: "u", api: api)
        await m.load()
        m.reply(to: m.threads![0])
        m.draft = "好的"
        api.fail = true
        await m.send()
        #expect(m.draft == "好的")
        #expect(m.target == .thread(id: "c1", name: "韩梅梅", text: "圆角统一成多少？"))
        #expect(m.writeError?.contains("飞书网关未登录飞书") == true)
        await m.send()
        #expect(api.writes[0].write?.clientID == api.writes[1].write?.clientID)
        api.fail = false
        m.draft = "好的，改了"
        await m.send()
        #expect(api.writes[2].write?.clientID != api.writes[1].write?.clientID)
        #expect(m.writeError == nil && m.draft.isEmpty)
    }

    @Test func blankWordsAreNotSent() async {
        let api = FakeAPI([])
        let m = FeishuDocsCommentsModel(url: "u", api: api)
        m.draft = " \n "
        #expect(!m.canSend)
        await m.send()
        #expect(api.writes.isEmpty)
    }

    @Test func resolvingMovesTheThreadAndReopeningBringsItBack() async {
        let api = FakeAPI([thread("c1"), thread("c2", resolved: true)])
        let m = FeishuDocsCommentsModel(url: "u", api: api)
        await m.load()
        await m.setResolved(m.threads(resolved: false)[0], true)
        #expect(m.threads(resolved: true).map(\.id).sorted() == ["c1", "c2"])
        await m.setResolved(m.threads(resolved: true).first { $0.id == "c2" }!, false)
        #expect(m.threads(resolved: false).map(\.id) == ["c2"])
        #expect(api.resolves.map { "\($0.thread)=\($0.resolved)" } == ["c1=true", "c2=false"])
        api.fail = true
        await m.setResolved(m.threads(resolved: false)[0], true)
        #expect(m.writeError?.hasPrefix("没能标为已解决") == true)
        #expect(m.resolving.isEmpty)
    }

    /// Only a hub whose plugin says it can write comments offers it; a hub that says nothing (unknown) does not.
    @Test func onlyAnExtensionThatSaysSoOffersComments() {
        #expect(LarkFeatures(hub: ["lark", "lark_doc_comments"]).docComments)
        #expect(!LarkFeatures(hub: ["lark"]).docComments)
        #expect(!LarkFeatures(hub: []).docComments)
        #expect(!LarkFeatures(hub: ["lark_doc_comments"]).docComments) // not without the plugin's lark
    }

    @Test func theWriteSaysWhatTheExtensionReads() throws {
        let w = FeishuDocsCommentWrite(url: "u", content: "@李雷 好", mentions: [LarkMention(userID: "ou_1", name: "李雷")],
                                       quote: "这段", blockID: "b2", clientID: "id-1")
        let json = try JSONSerialization.jsonObject(with: JSONEncoder().encode(w)) as! [String: Any]
        #expect(json["block_id"] as? String == "b2" && json["client_id"] as? String == "id-1" && json["quote"] as? String == "这段")
        #expect((json["mentions"] as? [[String: String]])?.first?["user_id"] == "ou_1")
    }
}
