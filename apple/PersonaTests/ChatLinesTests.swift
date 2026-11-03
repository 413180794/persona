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
}
