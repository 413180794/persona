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
        // DEBUG -openAllWork flips every line: the opened one closes.
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

    /// A running turn's words between its steps: the latest out of the work line; while the next ones stream in (drawn
    /// after the lines), in it already, so the chat does not grow under the new words and then shrink when they land.
    @Test func theRunningTurnsLatestWordsFoldWhileTheNextStream() {
        let items = [item(1, .text, role: "user", text: "查一下"), item(2, .toolUse, tool: "Bash", id: "a"),
                     item(3, .toolResult, role: "user", text: "ok", id: "a"), item(4, .text, text: "先看了第一处"),
                     item(5, .toolUse, tool: "Read", id: "b"), item(6, .toolResult, role: "user", text: "ok", id: "b")]
        let b = blocks(items)
        guard case .turn(let t) = b.last else { Issue.record("expected a turn"); return }
        #expect(kinds(ChatLines(openWork: []).build(b, live: t.id)) == ["user", "work", "answer"])
        let streaming = ChatLines(openWork: []).build(b, live: t.id, streaming: true)
        #expect(kinds(streaming) == ["user", "work"])
        guard case .work(_, let steps, _, _, _) = streaming[1].content else { Issue.record("expected a work line"); return }
        #expect(steps.count == 3, "the words are a step of the line now")
        // A finished turn has no streaming words of its own: as before.
        #expect(kinds(ChatLines(openWork: []).build(b, live: nil, streaming: true)) == ["user", "work", "answer"])
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

    /// Recaps in a row are one line (newest last in it, the first one's id); a recap with something after it stays.
    @Test func recapsInARowFold() {
        let items = [item(1, .text, role: "user", text: "看一下"), item(2, .text, text: "好了"),
                     item(3, .recap, text: "第一次"), item(4, .recap, text: "第二次"), item(5, .recap, text: "第三次")]
        let lines = ChatLines(openWork: []).build(blocks(items), live: nil)
        guard case .recaps(let id, let recaps)? = lines.last?.content else {
            Issue.record("no merged recaps: \(lines.map(\.id))")
            return
        }
        #expect(recaps.map(\.text) == ["第一次", "第二次", "第三次"])
        #expect(id == "rc-3#0")
        #expect(lines.last?.holds("5#0") == true)

        let apart = [item(1, .recap, text: "一"), item(2, .text, role: "user", text: "在吗"), item(3, .text, text: "在"), item(4, .recap, text: "二")]
        let kept = ChatLines(openWork: []).build(blocks(apart), live: nil)
        #expect(kept.filter { if case .row(.recap) = $0.content { true } else { false } }.count == 2)
        #expect(!kept.contains { if case .recaps = $0.content { true } else { false } })
    }

    /// The newest recap is drawn open as the chat's last line (#1360), in the flow: only while it is the last line and
    /// still pending; the earlier recaps of a folded line stay a chip above it.
    @Test func theOpenRecapIsTheLastLine() {
        let one = ChatLines(openWork: []).build(blocks([item(1, .text, role: "user", text: "看一下"), item(2, .text, text: "好了"),
                                                       item(3, .recap, text: "回顾")]), live: nil)
        let recap = one.last.flatMap { if case .row(.recap(let i)) = $0.content { i } else { nil } }
        let open = ChatLines.openRecap(one, pending: recap)
        #expect(open?.line == one.last?.id)
        #expect(open?.earlier.isEmpty == true)
        #expect(open?.newest.text == "回顾")
        #expect(ChatLines.openRecap(one, pending: nil) == nil, "closed: the one-line marker")

        let three = ChatLines(openWork: []).build(blocks([item(1, .text, role: "user", text: "看一下"), item(2, .text, text: "好了"),
                                                         item(3, .recap, text: "一"), item(4, .recap, text: "二"), item(5, .recap, text: "三")]), live: nil)
        guard case .recaps(_, let items)? = three.last?.content else {
            Issue.record("no merged recaps: \(three.map(\.id))")
            return
        }
        let folded = ChatLines.openRecap(three, pending: items.last)
        #expect(folded?.line == three.last?.id)
        #expect(folded?.earlier.map(\.text) == ["一", "二"])
        #expect(folded?.newest.text == "三")
        #expect(ChatLines.openRecap(three, pending: items.first) == nil, "only the newest one opens")

        // Something after it: no longer the newest thing, it is not open.
        let after = ChatLines(openWork: []).build(blocks([item(1, .recap, text: "回顾"), item(2, .text, role: "user", text: "在吗"),
                                                         item(3, .text, text: "在")]), live: nil)
        let earlier = after.first.flatMap { if case .row(.recap(let i)) = $0.content { i } else { nil } }
        #expect(earlier != nil)
        #expect(ChatLines.openRecap(after, pending: earlier) == nil)
    }

    /// A picture sent with a message to Jarvis: claude -p writes the message's line with the picture first and the
    /// text after it (an image block, then the text), where a pasted one in an Agents chat comes after the text. The
    /// picture belongs to that message, above its bubble, as it showed while it was being sent: it went to the
    /// exchange before (folded into its work, out of sight).
    @Test func aPictureSentBeforeItsTextStaysWithItsMessage() {
        var photo = item(3, .image, role: "user", id: nil)
        photo.image = ImagePayload(mime: "image/png", data: "iVBORw0KGgo=")
        var text = item(3, .text, role: "user", text: "看看这张")
        text.id = "3#1"
        let items = [item(1, .text, role: "user", text: "早"), item(2, .text, text: "早上好"), photo, text, item(4, .text, text: "是一张截图")]
        let lines = ChatLines(openWork: []).build(blocks(items), live: nil)
        #expect(kinds(lines) == ["user", "answer", "other", "user", "answer"], "\(lines.map(\.id))")
        guard case .userImages(_, let shown)? = lines.first(where: { if case .userImages = $0.content { true } else { false } })?.content else {
            Issue.record("no picture line: \(lines.map(\.id))")
            return
        }
        #expect(shown.map(\.id) == ["3#0"])
    }

    /// The same picture after its text (a pasted image in an Agents chat) shows as it did.
    @Test func aPictureAfterItsTextStaysWithItsMessage() {
        var photo = item(3, .image, role: "user", id: nil)
        photo.image = ImagePayload(mime: "image/png", data: "iVBORw0KGgo=")
        photo.id = "3#1"
        let items = [item(1, .text, role: "user", text: "早"), item(2, .text, text: "早上好"),
                     item(3, .text, role: "user", text: "看看这张"), photo, item(4, .text, text: "是一张截图")]
        #expect(kinds(ChatLines(openWork: []).build(blocks(items), live: nil)) == ["user", "answer", "other", "user", "answer"])
    }

    /// A picture sent to Jarvis with no text: its line holds the picture alone. It is a message of its own, shown as
    /// a sent picture, not the end of the exchange before it.
    @Test func aPictureSentAloneIsAMessageOfItsOwn() {
        var photo = item(3, .image, role: "user", id: nil)
        photo.image = ImagePayload(mime: "image/png", data: "iVBORw0KGgo=")
        let items = [item(1, .text, role: "user", text: "早"), item(2, .text, text: "早上好"), photo, item(4, .text, text: "是一张截图")]
        let lines = ChatLines(openWork: []).build(blocks(items), live: nil)
        #expect(kinds(lines) == ["user", "answer", "other", "answer"], "\(lines.map(\.id))")
    }

    // MARK: Compactions (a divider, `Item.compact`)

    private func notice(_ seq: Int, _ text: String) -> Item { item(seq, .notice, role: "system", text: text) }

    private func compactRows(_ lines: [ChatLine]) -> [Item] {
        lines.compactMap { if case .row(.notice(let i)) = $0.content, i.compact != nil { i } else { nil } }
    }

    /// Compacted while idle, after a finished turn: one divider, no work line of notices to open (the user's screenshot:
    /// 「上下文已压缩」「已从压缩的上下文继续」 and the idle note in a 「收起 3 步」 work line).
    @Test func anIdleCompactionIsOneDivider() {
        let items = [item(1, .text, role: "user", text: "看一下"), item(2, .text, text: "看完了"),
                     notice(3, ChatStore.compactedText), notice(4, ChatStore.continuedText),
                     notice(5, "Compacted while idle, before the prompt cache expired · turn off with \"idleCompaction\": false in settings.json")]
        let lines = ChatLines(openWork: []).build(blocks(items), live: nil)
        #expect(kinds(lines) == ["user", "answer", "row"], "\(lines.map(\.id))")
        let c = compactRows(lines)
        #expect(c.count == 1)
        #expect(c.first?.compact?.trigger == "idle")
        #expect(c.first?.compact?.source == "空闲时")
        #expect(c.first?.compact?.notes?.count == 3)
    }

    /// /compact: the hub's trigger kept, the summary folded in, a divider after the command.
    @Test func aManualCompactionIsADividerAfterTheCommand() {
        var boundary = notice(2, ChatStore.compactedText)
        boundary.compact = Compaction(trigger: "manual", preTokens: 151_467)
        let items = [item(1, .text, role: "user", text: "/compact"), boundary, notice(3, ChatStore.continuedText)]
        let lines = ChatLines(openWork: []).build(blocks(items), live: nil)
        #expect(kinds(lines) == ["user", "row"], "\(lines.map(\.id))")
        #expect(compactRows(lines).first?.compact?.source == "手动")
        #expect(compactRows(lines).first?.compact?.notes == [ChatStore.compactedText, ChatStore.continuedText])
    }

    /// Compacted in the middle of work: it stays in the work line, as one step (not two notices).
    @Test func aCompactionMidTurnIsOneStepOfItsWork() {
        let items = [item(1, .text, role: "user", text: "做"), item(2, .toolUse, tool: "Bash", id: "a"), item(3, .toolResult, role: "user", text: "ok", id: "a"),
                     notice(4, ChatStore.compactedText), notice(5, ChatStore.continuedText),
                     item(6, .toolUse, tool: "Read", id: "b"), item(7, .toolResult, role: "user", text: "ok", id: "b"), item(8, .text, text: "好了")]
        let lines = ChatLines(openWork: []).build(blocks(items), live: nil)
        #expect(kinds(lines) == ["user", "work", "answer"])
        guard case .work(_, let work, _, _, _)? = lines.first(where: { if case .work = $0.content { true } else { false } })?.content else {
            Issue.record("no work line")
            return
        }
        #expect(work.filter(ChatLines.isCompaction).count == 1)
    }

    /// The summary without its boundary (on an older page) is a divider by itself; Codex's lone notice too.
    @Test func aSummaryAloneOrACodexNoticeIsADivider() {
        let alone = ChatLines(openWork: []).build(blocks([notice(1, ChatStore.continuedText), item(2, .text, role: "user", text: "继续")]), live: nil)
        #expect(compactRows(alone).count == 1)
        let codex = ChatLines(openWork: []).build(blocks([item(1, .text, role: "user", text: "a"), item(2, .text, text: "b"), notice(3, ChatStore.compactedText)]), live: nil)
        #expect(compactRows(codex).count == 1)
    }

    /// The same after a turn that did work: the divider comes after the answer, not hidden in its folded work line
    /// (the Jarvis chat showed nothing of the compaction).
    @Test func anIdleCompactionAfterWorkIsAfterTheAnswer() {
        let items = [item(1, .text, role: "user", text: "看一下"), item(2, .toolUse, tool: "Bash", id: "a"), item(3, .toolResult, role: "user", text: "ok", id: "a"),
                     item(4, .text, text: "看完了"), notice(5, ChatStore.compactedText), notice(6, ChatStore.continuedText)]
        let lines = ChatLines(openWork: []).build(blocks(items), live: nil)
        #expect(kinds(lines) == ["user", "work", "answer", "row"], "\(lines.map(\.id))")
        guard case .work(_, let work, _, _, _)? = lines.first(where: { if case .work = $0.content { true } else { false } })?.content else { return }
        #expect(!work.contains(where: ChatLines.isCompaction))
    }
}
