import Foundation
import Testing
@testable import Jarvis

/// Pure logic behind AskJarvisIntent / AgentTaskIntent / AgentsStatusIntent.
struct SiriIntentsTests {
    private func item(_ seq: Int, role: String, kind: String = "text", text: String?) -> Item {
        var obj: [String: Any] = ["id": "i\(seq)", "seq": seq, "role": role, "kind": kind]
        if let text { obj["text"] = text }
        let data = try! JSONSerialization.data(withJSONObject: obj)
        return try! JSON.decoder.decode(Item.self, from: data)
    }

    @Test func answerJoinsAssistantTextOnly() {
        let items = [
            item(1, role: "user", text: "几点开会"),
            item(2, role: "assistant", kind: "thinking", text: "hmm"),
            item(3, role: "assistant", kind: "tool_use", text: nil),
            item(4, role: "assistant", text: "  下午三点。 "),
            item(5, role: "assistant", text: "在 3 楼。"),
        ]
        #expect(AskPersonaText.answer(in: items) == "下午三点。\n\n在 3 楼。")
        #expect(AskPersonaText.answer(in: [item(1, role: "user", text: "x")]) == nil)
    }

    @Test func turnOverOnlyWhenNotActive() {
        #expect(!AskPersonaText.turnOver(status: nil))
        #expect(!AskPersonaText.turnOver(status: .working))
        #expect(!AskPersonaText.turnOver(status: .blocked))
        #expect(AskPersonaText.turnOver(status: .idle))
        #expect(AskPersonaText.turnOver(status: .done))
    }

    @Test func spokenFallsBackAndClips() {
        #expect(AskPersonaText.spoken(nil) == AskPersonaText.pending)
        #expect(AskPersonaText.spoken(String(repeating: "字", count: 400)).count == 301)
    }

    private let agents = [
        PersonalAgentEntity(id: "ob:a", title: "写周报", provider: "claude", unread: 0, canSend: false),
        PersonalAgentEntity(id: "ob:b", title: "查数据", provider: "codex", unread: 2),
        PersonalAgentEntity(id: "ob:c", title: "日程", provider: "claude", unread: 1),
    ]

    @Test func pickerOrdersUnreadFirstAndSkipsReadOnly() {
        #expect(PersonalAgentIntentPicker.suggested(agents).map(\.id) == ["ob:b", "ob:c", "ob:a"])
        #expect(PersonalAgentIntentPicker.defaultTarget(agents)?.id == "ob:b")
        #expect(PersonalAgentIntentPicker.matching("codex", in: agents).map(\.id) == ["ob:b"])
        #expect(PersonalAgentIntentPicker.matching(" ", in: agents).count == 3)
    }

    @Test func statusWording() {
        #expect(PersonalAgentIntentText.status([]) == "没有个人 Agent 会话。")
        #expect(PersonalAgentIntentText.status(agents) == "2 个会话共 3 条未读：「查数据」2 条，「日程」1 条")
        let read = [PersonalAgentEntity(id: "x", title: "甲")]
        #expect(PersonalAgentIntentText.status(read) == "没有未读。最近的是：「甲」")
    }

    @Test func sentWording() {
        #expect(PersonalAgentIntentText.sent(status: "sent", to: "甲", error: nil) == "已交给「甲」")
        #expect(PersonalAgentIntentText.sent(status: "failed", to: "甲", error: "boom") == "发给「甲」失败：boom")
        #expect(PersonalAgentIntentText.name(title: " ", id: "ob:x") == "ob:x")
    }
}
