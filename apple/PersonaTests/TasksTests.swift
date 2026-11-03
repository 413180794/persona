import Foundation
import Testing
@testable import Jarvis

/// Tasks from the assistant's conversation (docs/unified-entry.md): the hub's shapes decode, a task's state follows
/// its agents, the feed lands at the end of the exchange it happened in, and the @ of a mention is told from an address.
@MainActor
struct TasksTests {
    private func decode<T: Decodable>(_ json: String, as: T.Type) throws -> T { try JSON.decoder.decode(T.self, from: Data(json.utf8)) }

    @Test func feedAndPlanDecode() throws {
        let res = try decode(#"""
        {"items":[
          {"id":"feed_1","conversation":"c1","at":"2026-11-03T10:00:00.123456789Z","kind":"plan","ref":"plan_1"},
          {"id":"feed_2","conversation":"c1","at":"2026-11-03T10:05:00Z","kind":"receipt","ref":"todo_1",
           "body":{"title":"修登录崩溃","state":"done","summary":"改好了","pr":"https://github.com/o/r/pull/901","machine":"box"}},
          {"id":"feed_3","conversation":"c1","at":"2026-11-03T10:06:00Z","kind":"answered","ref":"todo_1",
           "body":{"question":"使用 Bash","answer":"批准了"}},
          {"id":"feed_4","conversation":"c1","at":"2026-11-03T10:07:00Z","kind":"something_new","ref":"x"}],
         "plans":{"plan_1":{"id":"plan_1","conversation":"c1","state":"open","created_at":"2026-11-03T10:00:00Z",
           "tasks":[{"title":"修登录崩溃","prompt":"…","machine":"box","cwd":"/code/app","readonly":false,"why":"最近在这"}]}}}
        """#, as: APIClient.FeedResponse.self)
        #expect(res.items.map(\.kind) == [.plan, .receipt, .answered, .unknown])
        let receipt = res.items[1]
        #expect(receipt.title == "修登录崩溃" && !receipt.failed && receipt.prLabel == "PR #901" && receipt.summary == "改好了")
        #expect(res.items[2].answer == "批准了")
        let plan = try #require(res.plans?["plan_1"])
        #expect(plan.state == .open && plan.tasks.first?.project == "app" && plan.tasks.first?.isReadOnly == false)
    }

    @Test func todoDecodesWithAndWithoutOrigin() throws {
        let t = try decode(#"""
        {"id":"todo_1","title":"修登录崩溃","notes":"","done_at":null,"created_at":"2026-11-03T10:00:00Z","origin":"c1","plan_id":"plan_1",
         "spaces":[],"agents":[{"machine_id":"box","session_id":"s1","linked_at":"2026-11-03T10:00:01Z","released_at":null,"title":"","cwd":"/code/app","agent":"claude","start_error":"","source":"manual","via":""},
                    {"machine_id":"box","session_id":"s0","released_at":"2026-11-03T09:00:00Z"}]}
        """#, as: TodoItem.self)
        #expect(t.origin == "c1" && t.planID == "plan_1" && t.activeAgents.map(\.sessionID) == ["s1"])
        let hand = try decode(#"{"id":"todo_2","title":"手动的","agents":[]}"#, as: TodoItem.self)
        #expect(hand.origin == nil && hand.agents.isEmpty)
    }

    @Test func stateFollowsTheAgents() {
        let todo = TodoItem(id: "t", title: "t")
        #expect(TaskState(todo: todo, agents: []) == .notStarted)
        #expect(TaskState(todo: todo, agents: [.working, .needsYou]) == .needsYou)
        #expect(TaskState(todo: todo, agents: [.working, .unread]) == .unread)
        #expect(TaskState(todo: todo, agents: [.working, .idle]) == .working)
        #expect(TaskState(todo: todo, agents: [.idle, .offline]) == .followup)
        var done = todo
        done.doneAt = .now
        #expect(TaskState(todo: done, agents: [.needsYou]) == .done)
        #expect(TaskState.needsYou.priority < TaskState.working.priority && TaskState.working.priority < TaskState.done.priority)
    }

    private func user(_ seq: Int, _ minutesAgo: Double) -> ChatLine {
        .row(.user(Item(id: "\(seq)#0", seq: seq, ts: Date(timeIntervalSinceNow: -minutesAgo * 60), role: "user", kind: .text,
                        text: "m\(seq)", toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)))
    }

    private func answer(_ seq: Int, _ minutesAgo: Double) -> ChatLine {
        .row(.assistant(Item(id: "\(seq)#0", seq: seq, ts: Date(timeIntervalSinceNow: -minutesAgo * 60), role: "assistant", kind: .text,
                             text: "a\(seq)", toolUseID: nil, toolName: nil, toolInput: nil, isError: nil, image: nil)))
    }

    private func feed(_ id: String, _ minutesAgo: Double) -> FeedItem {
        FeedItem(id: id, conversation: "c1", at: Date(timeIntervalSinceNow: -minutesAgo * 60), kind: .task, ref: "t", body: nil)
    }

    /// A feed item goes after its turn's answer (not between the message and the answer), before the next message;
    /// what came after the last message goes last.
    @Test func feedLandsAtTheEndOfItsExchange() {
        let lines = [user(1, 50), answer(2, 49), user(3, 30), answer(4, 29)]
        let merged = ChatLines.mergeFeed(lines, [feed("early", 49.5), feed("mid", 20), feed("late", 1)])
        #expect(merged.map(\.id) == ["1#0", "2#0", "feed-early", "3#0", "4#0", "feed-mid", "feed-late"])
        #expect(ChatLines.mergeFeed(lines, []).map(\.id) == lines.map(\.id))
    }

    @Test func mentionIsTheTrailingAt() {
        func hit(_ s: String) -> String? { TaskMentionList.mention(in: s).map { String(s[$0]) } }
        #expect(hit("@") == "@")
        #expect(hit("也改一下 @登录") == "@登录")
        #expect(hit("发到 me@example.com") == nil) // an address, not a mention
        #expect(hit("@登录 好了") == nil) // the mention ended with a space
        #expect(hit("没有") == nil)
    }
}
