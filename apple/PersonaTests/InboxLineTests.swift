import Foundation
import Testing
@testable import Persona

/// Thread (#2128): the hub's tree decoded, its rows in reading order, and the route asked.
struct InboxLineTests {
    private func item(_ id: String, _ title: String) -> String {
        #"{"id":"\#(id)","source":"examplechat","kind":"group","account":"examplechat","account_name":"Example Chat","title":"\#(title)","preview":"","count":1}"#
    }

    @Test func decodesTheTreeAndAnItemsParent() throws {
        let json = """
        {"node":{"kind":"inbox","id":"b"},"item":\(item("b", "示例 B").dropLast()),"parent":{"kind":"inbox","id":"a","by":"continues"}},
         "ancestors":[{"kind":"inbox","id":"a","by":"continues","item":\(item("a", "示例 A"))},{"kind":"todo","id":"t1","by":"user"}],
         "children":[\(item("c", "示例 C"))]}
        """
        let tree = try JSON.decoder.decode(InboxTree.self, from: Data(json.utf8))
        #expect(tree.item?.parent == InboxParent(kind: "inbox", id: "a", by: "continues"))
        #expect(tree.ancestors.map(\.parent.id) == ["a", "t1"])
        #expect(tree.ancestors[0].item?.title == "示例 A" && tree.ancestors[1].item == nil)
        #expect(tree.children.map(\.id) == ["c"])

        let rows = InboxLine.rows(tree, todoTitle: { $0 == "t1" ? "示例待办" : nil })
        #expect(rows.map { [$0.label, $0.title] } == [["待办", "示例待办"], ["事件", "示例 A"], ["下一步", "示例 C"]])
        #expect(rows.map(\.up) == [true, true, false])
        #expect(rows[0].target == .todo("t1"))
        guard case .item(let a)? = rows[1].target, case .item(let c)? = rows[2].target else {
            Issue.record("items open as items")
            return
        }
        #expect(a.id == "a" && c.id == "c")
    }

    @Test func aSessionOpensItsChatAndAGoneItemOpensNothing() {
        let tree = InboxTree(node: InboxParent(kind: "inbox", id: "b"), ancestors: [
            .init(parent: InboxParent(kind: "inbox", id: "gone")),
            .init(parent: InboxParent(kind: "session", id: "m1/s1")),
            .init(parent: InboxParent(kind: "session", id: "nosplit")),
        ])
        let rows = InboxLine.rows(tree, sessionTitle: { $0.sessionID == "s1" ? "示例会话" : nil })
        #expect(rows.map(\.title) == ["nosplit", "示例会话", "已不在收件箱里"])
        #expect(rows[0].target == nil)
        #expect(rows[1].target == .session(SessionKey(machineID: "m1", sessionID: "s1")) && rows[1].label == "Agent 会话")
        #expect(rows[2].target == nil)
        #expect(InboxLine.rows(InboxTree(node: InboxParent(kind: "inbox", id: "x"))).isEmpty) // no line: nothing drawn
    }

    @Test func asksTheTreeRouteWithIdsKeptReadable() {
        #expect(APIClient.inboxTreePath("inbox", "lark:F1:900") == "/api/v1/inbox/tree/inbox/lark:F1:900")
        #expect(APIClient.inboxTreePath("session", "m 1/s1") == "/api/v1/inbox/tree/session/m%201/s1")
        #expect(APIClient.inboxTreePath("inbox", "gh:o/r#1") == "/api/v1/inbox/tree/inbox/gh:o%2Fr%231")
    }
}
