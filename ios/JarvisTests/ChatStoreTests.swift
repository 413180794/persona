import Testing
@testable import Jarvis

@MainActor
struct ChatStoreTests {
    private func item(_ seq: Int, _ kind: ItemKind, role: String = "assistant", text: String? = nil, tool: String? = nil, id: String? = nil) -> Item {
        Item(id: "\(seq)#0", seq: seq, ts: nil, role: role, kind: kind, text: text, toolUseID: id, toolName: tool, toolInput: nil, isError: nil, image: nil)
    }

    @Test func groupsToolRunsAndAttachesResults() {
        let items = [
            item(1, .text, role: "user", text: "hi"),
            item(2, .thinking, text: "plan"),
            item(3, .toolUse, tool: "Bash", id: "a"),
            item(4, .toolResult, role: "user", text: "ok", id: "a"),
            item(5, .thinking, text: "next"),
            item(6, .toolUse, tool: "Read", id: "b"),
            item(7, .text, text: "done"),
        ]
        let rows = ChatStore.buildRows(items)
        #expect(rows.count == 4)
        guard case .tools(let steps) = rows[2] else { Issue.record("expected tools"); return }
        #expect(steps.count == 3)
        if case .call(let c) = steps[0] { #expect(c.result?.text == "ok") } else { Issue.record("expected call") }
    }

    @Test func parsesSpinnerAndOptions() {
        let screen = """
        ⏺ Working on it
        ✻ Cogitating… (12s · ↓ 3.2k tokens · esc to interrupt)
        ──────────────────────────────────────────
        ❯ 1. Yes
          2. No, exit
        """
        #expect(ScreenParser.spinner(screen) == "Cogitating… (12s · ↓ 3.2k tokens)")
        let opts = ScreenParser.options(screen)
        #expect(opts.map(\.key) == ["1", "2"])
        #expect(opts.first?.selected == true)
    }

    @Test func pairURL() {
        let c = ServerConfig(pairURL: "jarvis://pair?url=http%3A%2F%2Fhost%3A18660&token=abc")
        #expect(c?.url.absoluteString == "http://host:18660")
        #expect(c?.token == "abc")
    }

    @Test func goDates() {
        #expect(JSON.parseDate("2026-10-24T11:15:05.251344371Z") != nil)
        #expect(JSON.parseDate("2026-10-24T11:15:05Z") != nil)
        #expect(JSON.parseDate("0001-01-01T00:00:00Z")?.isMeaningful == false)
    }
}

struct FilePathTests {
    @Test func keepsTildeAndFindsPaths() {
        let text = "cat > ~/.config/systemd/user/jarvisd.service 并读 /tmp/jarvis-tasks/05-image-viewer.md 和 ios/Jarvis/Foo.swift，网址 https://example.com/a/b 不算"
        let paths = FilePaths.find(in: text)
        #expect(paths == ["~/.config/systemd/user/jarvisd.service", "/tmp/jarvis-tasks/05-image-viewer.md", "ios/Jarvis/Foo.swift"])
        #expect(FilePaths.find(in: "模型 deepseek/v4.1-flash-chain 和 model_hub/es1_orange_o50").isEmpty)
    }
}

@MainActor
struct TurnTests {
    private func item(_ seq: Int, _ kind: ItemKind, role: String = "assistant", text: String? = nil, tool: String? = nil, id: String? = nil, queued: Bool? = nil) -> Item {
        Item(id: "\(seq)#0", seq: seq, ts: nil, role: role, kind: kind, text: text, toolUseID: id, toolName: tool, toolInput: nil, isError: nil, image: nil, queued: queued)
    }

    @Test func finishedTurnGathersQueuedMessagesAndKeepsTheLastAnswer() {
        let items = [
            item(1, .text, role: "user", text: "do it"),
            item(2, .text, text: "你好，我先看看"),
            item(3, .toolUse, tool: "Bash", id: "a"),
            item(4, .toolResult, role: "user", text: "ok", id: "a"),
            item(5, .text, role: "user", text: "also this", queued: true),
            item(6, .thinking, text: "hmm"),
            item(7, .toolUse, tool: "Read", id: "b"),
            item(8, .text, text: "done: here is the result"),
            item(9, .recap, role: "system", text: "recap"),
            item(10, .text, role: "user", text: "next"),
            item(11, .text, text: "sure"),
        ]
        let blocks = ChatBlock.build(ChatStore.buildRows(items))
        #expect(blocks.count == 3)
        guard case .turn(let t) = blocks[0] else { Issue.record("turn expected"); return }
        #expect(t.users.count == 2)
        #expect(t.toolCount == 2)
        #expect(t.interimCount == 1)
        if case .assistant(let a) = t.answer { #expect(a.text == "done: here is the result") } else { Issue.record("answer") }
        if case .row(.recap) = blocks[1] {} else { Issue.record("recap stays standalone") }
    }

    @Test func compactionMidTurnDoesNotSplitIt() {
        let items = [
            item(1, .text, role: "user", text: "do it"),
            item(2, .toolUse, tool: "Bash", id: "a"),
            item(3, .notice, role: "system", text: "上下文已压缩"),
            item(4, .toolUse, tool: "Read", id: "b"),
            item(5, .text, text: "done"),
        ]
        let blocks = ChatBlock.build(ChatStore.buildRows(items))
        #expect(blocks.count == 1)
        guard case .turn(let t) = blocks[0] else { Issue.record("turn expected"); return }
        #expect(t.users.count == 1)
        #expect(t.toolCount == 2)
    }

    @Test func bubbleHidesTheImageNote() {
        #expect(UserBubble.display("看这个\n\n相关图片：/a/1.jpg、/a/2.jpg") == "看这个")
        #expect(UserBubble.display("看这个\n\n（我从手机发来了图片，请用 Read 查看：/a/1.jpg）") == "看这个")
        #expect(UserBubble.display("相关图片：/a/1.jpg").isEmpty) // only photos: thumbnails, no bubble
        #expect(UserBubble.imagePaths("看这个\n\n相关图片：/a/1.jpg、/a/2.jpg") == ["/a/1.jpg", "/a/2.jpg"])
        #expect(UserBubble.imagePaths("看\n\n（我从手机发来了图片，请用 Read 查看：/a/1.jpg）") == ["/a/1.jpg"])
        #expect(UserBubble.imagePaths("没有图片").isEmpty)
        let tagged = "看这个\n\n<attached_images>\n/a/1.jpg\n/a/2.jpg\n</attached_images>"
        #expect(UserBubble.display(tagged) == "看这个")
        #expect(UserBubble.imagePaths(tagged) == ["/a/1.jpg", "/a/2.jpg"])
        // typed mid-sentence, not the note
        #expect(UserBubble.display("改成，相关图片：xx 就行") == "改成，相关图片：xx 就行")
    }

    @Test func cacheKeepsTheLastTwoExchanges() {
        let items = [
            item(1, .text, role: "user", text: "one"), item(2, .text, text: "a1"),
            item(3, .text, role: "user", text: "two"), item(4, .text, role: "user", text: "two more", queued: true),
            item(5, .text, text: "a2"),
            item(6, .text, role: "user", text: "three"), item(7, .text, text: "a3"),
        ]
        #expect(ChatCache.tail(items, turns: 2).map(\.seq) == [3, 4, 5, 6, 7])
    }
}

