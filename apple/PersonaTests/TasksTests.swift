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

    private func toolCall(_ name: String, result: String?, error: Bool = false, seq: Int = 10) -> ToolCall {
        let use = Item(id: "\(seq)#0", seq: seq, ts: .now, role: "assistant", kind: .toolUse, text: nil, toolUseID: "tu\(seq)",
                       toolName: name, toolInput: nil, isError: nil, image: nil)
        let res = result.map { Item(id: "\(seq + 1)#0", seq: seq + 1, ts: .now, role: "user", kind: .toolResult, text: $0, toolUseID: "tu\(seq)",
                                    toolName: nil, toolInput: nil, isError: error, image: nil) }
        return ToolCall(use: use, result: res)
    }

    @Test func quickResultsComeFromThePhonesWrites() throws {
        let r = try #require(QuickResult(toolCall("mcp__jarvis__phone_reminders_create",
                                                  result: #"{"id":"R1","title":"给爸妈打电话","due":"2026-11-07T10:00:00+08:00","list":"提醒事项"}"#)))
        #expect(r.kind == .reminder && r.itemID == "R1" && r.title == "给爸妈打电话" && r.place == "提醒事项" && r.when != nil)
        let e = try #require(QuickResult(toolCall("phone_calendar_create_event",
                                                  result: #"{"id":"E1","title":"评审","start":"2026-11-03T15:00:00+08:00","calendar":"工作"}"#)))
        #expect(e.kind == .event && e.place == "工作")
        #expect(QuickResult(toolCall("mcp__jarvis__phone_reminders_list", result: #"{"id":"x"}"#)) == nil) // a read is no result card
        #expect(QuickResult(toolCall("mcp__jarvis__phone_reminders_create", result: #"{"error":"denied"}"#, error: true)) == nil)
        #expect(QuickResult(toolCall("mcp__jarvis__phone_reminders_create", result: nil)) == nil) // still running
    }

    /// A turn's quick result goes right after its work line, after the line's open steps.
    @Test func quickResultFollowsItsWork() {
        let call = toolCall("mcp__jarvis__phone_reminders_create", result: #"{"id":"R1","title":"t"}"#)
        let work = ChatLine.work(id: "wt-1", items: [.step(.call(call))], live: false, start: .now, end: .now)
        let step = ChatLine.workStep(group: "wt-1", item: .step(.call(call)), first: true)
        let end = ChatLine.workEnd(group: "wt-1", count: 1)
        let lines = [user(1, 5), work, answer(2, 4)]
        #expect(ChatLines.quickResults(lines).map(\.id) == ["1#0", "wt-1", "quick-10#0", "2#0"])
        let open = [user(1, 5), work, step, end, answer(2, 4)]
        #expect(ChatLines.quickResults(open).map(\.id) == ["1#0", "wt-1", step.id, end.id, "quick-10#0", "2#0"])
    }

    @Test func aLineForEachDay() {
        let lines = [user(1, 60 * 26), answer(2, 60 * 26 - 1), user(3, 5), answer(4, 4)]
        let ids = ChatLines.daySections(lines).map(\.id)
        #expect(ids.filter { $0.hasPrefix("day-") }.count == 2)
        #expect(ids.first?.hasPrefix("day-") == true && ids[3].hasPrefix("day-") && ids[4] == "3#0")
        let now = Date.now
        #expect(DayLine.label(now, now: now).hasPrefix("今天 · "))
        #expect(DayLine.label(now.addingTimeInterval(-86_400), now: now).hasPrefix("昨天 · "))
    }

    @Test func todayBodyDecodes() throws {
        let item = try decode(#"""
        {"id":"f","conversation":"c1","at":"2026-11-03T00:00:00+08:00","kind":"today","ref":"2026-11-03",
         "body":{"date":"2026-11-03","done":[{"id":"t1","title":"博客迁移"}],"working":2,
                 "meetings":[{"start":"2026-11-03T15:00:00+08:00","end":"2026-11-03T16:00:00+08:00","title":"设计评审"}],
                 "at_me":[{"name":"发布群","unread":3,"preview":"能赶上吗"}],"unread":4}}
        """#, as: FeedItem.self)
        let body = try #require(item.today)
        #expect(item.kind == .today && body.done?.first?.title == "博客迁移" && body.working == 2 && body.meetings?.first?.title == "设计评审")
        #expect(body.atMe?.first?.name == "发布群" && body.unread == 4 && body.waiting == nil)
    }
}

