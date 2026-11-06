import Foundation
import Testing
@testable import Persona

/// The Mac inbox's sidebar @Me filter is the iPhone's (`InboxView.matches`): docs and the Moments posts whose
/// newest notice @-mentions the user count; other company-circle posts do not (#3889).
@MainActor
struct MacInboxFilterTests {
    private func item(_ id: String, kind: String, momentNotice: String? = nil, docNotice: String? = nil) throws -> InboxItem {
        var extra = ""
        if let momentNotice { extra += ",\"moment\":{\"post_id\":\"p-\(id)\",\"notice\":\"\(momentNotice)\"}" }
        if let docNotice { extra += ",\"doc\":{\"url\":\"https://example.com/d\",\"type\":\"docx\",\"notice\":\"\(docNotice)\"}" }
        let json = """
        {"id":"\(id)","source":"lark","kind":"\(kind)","account":"feishu","account_name":"飞书","title":"\(id)","preview":"","count":1\(extra)}
        """
        return try JSON.decoder.decode(InboxItem.self, from: Data(json.utf8))
    }

    @Test func mentionFilterIncludesDocsAndAtMeMoments() throws {
        let state = MacInboxState()
        // Docs already showed in @Me before the fix.
        let doc = try item("d", kind: "doc", docNotice: "mention")
        // A company-circle post that @-mentions me (atInPost / atInComment) must show too.
        let atInPost = try item("p", kind: "moment", momentNotice: "atInPost")
        let atInComment = try item("c", kind: "moment", momentNotice: "atInComment")
        #expect(state.matches(doc, kind: "mention"))
        #expect(state.matches(atInPost, kind: "mention"))
        #expect(state.matches(atInComment, kind: "mention"))
    }

    @Test func companyCirclePostsWithoutAnAtMeStayOutOfTheMentionFilter() throws {
        let state = MacInboxState()
        let comment = try item("c", kind: "moment", momentNotice: "comment")
        let reply = try item("r", kind: "moment", momentNotice: "reply")
        let atInPost = try item("p", kind: "moment", momentNotice: "atInPost")
        #expect(!state.matches(comment, kind: "mention"))
        #expect(!state.matches(reply, kind: "mention"))
        // An @-mentioned moment is still not a private chat.
        #expect(!state.matches(atInPost, kind: "dm"))
    }
}
