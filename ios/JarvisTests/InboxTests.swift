import Foundation
import Testing
@testable import Jarvis

@MainActor
struct InboxTests {
    private let json = """
    {"items":[
      {"id":"lark:f1:11","source":"lark","kind":"mention","account":"feishu","account_name":"飞书","title":"发布群","sender":"张三",
       "avatar":"/api/v1/lark/media/a","preview":"今晚能上线吗","count":3,"started_at":"2026-10-27T01:00:00Z","updated_at":"2026-10-27T02:00:00Z",
       "lark":{"feed_id":"f1","is_group":true,"start_position":"11","mention_position":"12"}},
      {"id":"mail:a1:abc:7","source":"mail","kind":"mail","account":"a1","account_name":"me@example.com","title":"年假确认",
       "sender":"HR","preview":"请在周五前回复","count":1,"updated_at":"2026-10-27T03:00:00Z","tier":"fyi","summary":"HR 让你确认年假",
       "done_at":"2026-10-27T04:00:00Z","done_by":"user",
       "mail":{"account_id":"a1","mailbox":"INBOX","uids":[5,7],"message_id":"<m@x>","from":{"name":"HR","email":"hr@x"},"direct":true}}
    ]}
    """

    private struct List: Decodable { var items: [InboxItem] }

    @Test func decodesItemsAndOpensThem() throws {
        let items = try JSON.decoder.decode(List.self, from: Data(json.utf8)).items
        #expect(items.count == 2)
        let chat = items[0], mail = items[1]
        #expect(!chat.isTriaged && chat.triage == .action) // not triaged yet: shown as needing you
        #expect(chat.kindLabel == "@我")
        let conv = try #require(chat.larkConversation)
        #expect(conv.id == "f1" && conv.isGroup && conv.focus == "11" && conv.unread == 3)

        #expect(mail.isDone && mail.triage == .fyi && mail.kindLabel == nil)
        let m = try #require(mail.mailSummary)
        #expect(m.uid == 7 && m.accountID == "a1" && m.subject == "年假确认" && m.seen)
        #expect(mail.larkConversation == nil && chat.mailSummary == nil)
    }

    @Test func feishuLinksOpenNatively() throws {
        let doc = try #require(URL(string: "https://open.feishu.cn/docx/ExampleDocument
        let post = try #require(URL(string: "https://applink.larkoffice.com/client/moments/detail?postId=7000000000000000001"))
        let minutes = try #require(URL(string: "https://open.feishu.cn/docx/ExampleDocument
        #expect(FeishuLinks.route(doc) == .doc(url: doc.absoluteString, title: nil))
        #expect(FeishuLinks.route(post) == .momentsPost(id: "7000000000000000001"))
        #expect(FeishuLinks.route(minutes) == nil) // a web page in the link sheet
    }
}
