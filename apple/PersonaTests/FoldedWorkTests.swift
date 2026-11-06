import Foundation
import Testing
@testable import Persona

/// Pages of long sessions come with the agent's work folded (`Item.fold`, docs/protocol.md「折叠的工作」): the work
/// line counts what a fold stands for, a fold replaces the steps it covers, and opening it puts the steps back.
@MainActor
struct FoldedWorkTests {
    private func item(_ seq: Int, _ kind: ItemKind, role: String = "assistant", text: String? = nil, tool: String? = nil) -> Item {
        Item(id: "\(seq)#0", seq: seq, ts: Date(timeIntervalSince1970: TimeInterval(seq)), role: role, kind: kind, text: text,
             toolUseID: tool, toolName: tool == nil ? nil : "Bash", toolInput: nil, isError: nil, image: nil)
    }

    private func fold(_ from: Int, _ through: Int, tools: Int, thoughts: Int = 0) -> Item {
        var f = item(from, .fold)
        f.id = "fold:\(from)#0"
        f.fold = Fold(items: through - from + 1, tools: tools, thoughts: thoughts, until: Date(timeIntervalSince1970: TimeInterval(through)),
                      throughSeq: through, throughID: "\(through)#0")
        return f
    }

    /// Steps 2…7: a thought, then three calls with their results (the result of the last one at 8 is outside).
    private func work() -> [Item] {
        [item(2, .thinking, text: "plan"),
         item(3, .toolUse, tool: "a"), item(4, .toolResult, role: "user", text: "ok", tool: "a"),
         item(5, .toolUse, tool: "b"), item(6, .toolResult, role: "user", text: "ok", tool: "b"),
         item(7, .toolUse, tool: "c"), item(8, .toolResult, role: "user", text: "ok", tool: "c")]
    }

    private func store() -> ChatStore { ChatStore(key: SessionKey(machineID: "test", sessionID: "fold-\(UUID())")) }

    /// Rows are rebuilt off the main actor: wait for the first exchange to show up (at most 2 s).
    private func rebuilt(_ s: ChatStore) async {
        for _ in 0..<200 {
            if case .turn = s.blocks.first { return }
            try? await Task.sleep(for: .milliseconds(10))
        }
    }

    @Test func aFoldCountsWhatItStandsFor() {
        let items = [item(1, .text, role: "user", text: "go"), fold(2, 8, tools: 3, thoughts: 1), item(9, .text, text: "done")]
        let blocks = ChatBlock.build(ChatStore.buildRows(items))
        guard case .turn(let t) = blocks.first else { Issue.record("no turn: \(blocks)"); return }
        #expect(t.toolCount == 3 && t.thoughtCount == 1)
        if case .assistant(let a) = t.answer { #expect(a.text == "done") } else { Issue.record("answer") }
        // The work ends where its last step did.
        #expect(t.process.last?.lastTS == Date(timeIntervalSince1970: 8))
    }

    @Test func aFoldReplacesTheStepsItCoversAndOpeningItPutsThemBack() async {
        let s = store()
        let prompt = item(1, .text, role: "user", text: "go"), answer = item(20, .text, text: "done")
        s.merge([prompt] + work() + [answer], backlog: true, hasMore: false)
        // A page with the same work folded (the latest page again): the steps give way to the fold.
        let f = fold(2, 8, tools: 3, thoughts: 1)
        s.merge([prompt, f, answer], backlog: true, hasMore: false)
        #expect(s.items.map(\.id) == ["1#0", "fold:2#0", "20#0"])
        s.merge([f]) // the same fold again changes nothing
        #expect(s.items.map(\.id) == ["1#0", "fold:2#0", "20#0"])
        // The turn went on (pushed steps), then a page folds the longer run: it replaces the old fold and the steps.
        let more = [item(9, .toolUse, tool: "d"), item(10, .toolResult, role: "user", text: "x", tool: "d")]
        s.merge(more)
        let longer = fold(2, 10, tools: 4, thoughts: 1)
        s.merge([prompt, longer, answer], backlog: true, hasMore: false)
        #expect(s.items.map(\.id) == ["1#0", "fold:2#0", "20#0"])
        #expect(s.items[1].fold?.tools == 4)

        // Opening it: the steps take the fold's place, in order.
        s.unfold(longer, into: work() + more)
        #expect(s.items.map(\.seq) == [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 20])
        await rebuilt(s)
        guard case .turn(let t) = s.blocks.first else { Issue.record("no turn"); return }
        #expect(t.toolCount == 4)
    }

    /// Work opened here stays open when a page folds it again. As a chat opens, the latest page can come twice (over
    /// HTTP and in the socket's backlog); the fold put back over its steps had already had its row fetch them, and the
    /// row stayed on "Loading N steps…" (#6038). A fold that reaches further covers steps not fetched here, and a page
    /// that starts over (a reload) has none of them: those fold the work again.
    @Test func workOpenedHereStaysOpenWhenAPageFoldsItAgain() {
        let s = store()
        let prompt = item(1, .text, role: "user", text: "go"), answer = item(20, .text, text: "done")
        let f = fold(2, 8, tools: 3, thoughts: 1), page = ItemSpan(history: "h", from: nil, to: 20)
        s.apply([prompt, f, answer], span: page, backlog: true, hasMore: false)
        s.unfold(f, into: work())
        s.apply([prompt, f, answer], span: page, backlog: true, hasMore: false)
        #expect(s.items.map(\.seq) == [1, 2, 3, 4, 5, 6, 7, 8, 20])
        s.apply([prompt, fold(2, 10, tools: 4, thoughts: 1), answer], span: page, backlog: true, hasMore: false)
        #expect(s.items.map(\.id) == ["1#0", "fold:2#0", "20#0"])
        s.replace(with: [prompt, f, answer], hasMore: false, span: page)
        #expect(s.items.map(\.id) == ["1#0", "fold:2#0", "20#0"] && s.items[1].fold?.throughSeq == 8)
    }

    /// A tool call outside the fold (still running when the page was cut) keeps its result when the result comes.
    @Test func stepsOutsideAFoldStay() {
        let s = store()
        s.merge([item(1, .text, role: "user", text: "go"), fold(2, 6, tools: 2, thoughts: 1), item(7, .toolUse, tool: "c")], backlog: true)
        s.merge([item(8, .toolResult, role: "user", text: "ok", tool: "c")])
        #expect(s.items.map(\.seq) == [1, 2, 7, 8])
        let rows = ChatStore.buildRows(s.items)
        guard case .tools(let steps) = rows.last else { Issue.record("rows \(rows)"); return }
        #expect(steps.count == 2)
        if case .call(let c) = steps.last { #expect(c.result?.text == "ok") } else { Issue.record("call") }
    }
}
