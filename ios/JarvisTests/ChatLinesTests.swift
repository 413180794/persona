import Foundation
import Testing
@testable import Jarvis

/// The chat's lines (`ChatLines`) from items, without the chat: one exchange per message, open work lines unfolded,
/// several messages of a finished turn merged, BeautifulUI's runs of tool calls.
@MainActor
struct ChatLinesTests {
    private func item(_ seq: Int, _ kind: ItemKind, role: String = "assistant", text: String? = nil, tool: String? = nil, id: String? = nil) -> Item {
        Item(id: "\(seq)#0", seq: seq, ts: nil, role: role, kind: kind, text: text, toolUseID: id, toolName: tool, toolInput: nil, isError: nil, image: nil)
    }

    private func blocks(_ items: [Item]) -> [ChatBlock] { ChatStore.rebuild(items, settled: nil).blocks }

    /// Each line's kind, short: user / answer / work / step / end / merged / split / run / error.
    private func kinds(_ lines: [ChatLine]) -> [String] {
        lines.map { line in
            switch line.content {
            case .row(.user): "user"
            case .row(.assistant): "answer"
            case .row: "row"
            case .work: "work"
            case .workStep: "step"
            case .workEnd: "end"
            case .mergedUsers: "merged"
            case .splitHeader: "split"
            case .toolRun: "run"
            case .apiError: "error"
            default: "other"
            }
        }
    }

    private let oneTurn = [
        { (s: ChatLinesTests) in s.item(1, .text, role: "user", text: "做一下") },
        { (s: ChatLinesTests) in s.item(2, .toolUse, tool: "Bash", id: "a") },
        { (s: ChatLinesTests) in s.item(3, .toolResult, role: "user", text: "ok", id: "a") },
        { (s: ChatLinesTests) in s.item(4, .toolUse, tool: "Read", id: "b") },
        { (s: ChatLinesTests) in s.item(5, .toolResult, role: "user", text: "ok", id: "b") },
        { (s: ChatLinesTests) in s.item(6, .text, text: "做完了") },
    ]

    @Test func aMessageItsWorkAndItsAnswer() {
        let lines = ChatLines(openWork: []).build(blocks(oneTurn.map { $0(self) }), live: nil)
        #expect(kinds(lines) == ["user", "work", "answer"])
        #expect(lines[1].id.hasPrefix("w-"))
    }

    @Test func anOpenWorkLineListsItsStepsThenEnds() {
        let closed = ChatLines(openWork: []).build(blocks(oneTurn.map { $0(self) }), live: nil)
        let open = ChatLines(openWork: [closed[1].id]).build(blocks(oneTurn.map { $0(self) }), live: nil)
        #expect(kinds(open) == ["user", "work", "step", "step", "end", "answer"])
        // DEBUG -openWork flips every line: the opened one closes.
        #expect(kinds(ChatLines(openWork: [closed[1].id], openAll: true).build(blocks(oneTurn.map { $0(self) }), live: nil))
                == ["user", "work", "answer"])
    }

    @Test func beautifulUIFoldsToolCallsInARowIntoOneRun() {
        let b = blocks(oneTurn.map { $0(self) })
        let work = ChatLines(openWork: []).build(b, live: nil)[1].id
        let lines = ChatLines(openWork: [work], chipRuns: true).build(b, live: nil)
        #expect(kinds(lines) == ["user", "work", "run", "end", "answer"])
        // A call that went to the background stays on its own line.
        let apart = ChatLines(openWork: [work], chipRuns: true, foldsIntoRun: { $0.use.toolUseID != "b" }).build(b, live: nil)
        #expect(kinds(apart) == ["user", "work", "step", "step", "end", "answer"])
    }

    @Test func aFinishedTurnOfSeveralMessagesReadsAsOne() {
        // The second message was sent while the agent worked (queued), so it joined the same turn.
        var queued = item(4, .text, role: "user", text: "再那样")
        queued.queued = true
        let items = [item(1, .text, role: "user", text: "先这样"), item(2, .toolUse, tool: "Bash", id: "a"),
                     item(3, .toolResult, role: "user", text: "ok", id: "a"), queued,
                     item(5, .toolUse, tool: "Read", id: "b"), item(6, .toolResult, role: "user", text: "ok", id: "b"),
                     item(7, .text, text: "都好了")]
        let merged = ChatLines(openWork: []).build(blocks(items), live: nil)
        #expect(kinds(merged) == ["merged", "work", "answer"])
        let split = ChatLines(openWork: [merged[1].id]).build(blocks(items), live: nil)
        #expect(kinds(split).first == "split")
        #expect(kinds(split).filter { $0 == "user" }.count == 2, "opened, each message is its own exchange again")
    }

    @Test func theRunningTurnIsNeverMerged() {
        var queued = item(3, .text, role: "user", text: "b")
        queued.queued = true
        let items = [item(1, .text, role: "user", text: "a"), item(2, .toolUse, tool: "Bash", id: "x"),
                     queued, item(4, .toolUse, tool: "Bash", id: "y")]
        let b = blocks(items)
        guard case .turn(let t) = b.last else { Issue.record("expected a turn"); return }
        #expect(!kinds(ChatLines(openWork: []).build(b, live: t.id)).contains("merged"))
    }
}
