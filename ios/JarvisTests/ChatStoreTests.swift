import Foundation
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

    @Test func agentOverridesItsMutedSpace() {
        var p = Pins()
        p.spaces = ["server/app"]
        #expect(p.mutes(session: "server/a", space: "server/app"))         // muted by its space
        p = p.togglingMute(session: "server/a", space: "server/app")       // 开启通知
        #expect(!p.mutes(session: "server/a", space: "server/app") && p.allowed == ["server/a"])
        #expect(p.mutes(session: "server/b", space: "server/app"))         // the others stay muted
        p = p.togglingMute(session: "server/a", space: "server/app")       // 屏蔽通知 again
        #expect(p.mutes(session: "server/a", space: "server/app") && p.sessions == ["server/a"] && p.allowed.isEmpty)
        p.spaces = []                                                       // space unmuted: own mute still holds
        #expect(p.mutes(session: "server/a", space: "server/app"))
        p = p.togglingMute(session: "server/a", space: "server/app")
        #expect(!p.mutes(session: "server/a", space: "server/app") && p.sessions.isEmpty && p.allowed.isEmpty)
    }

    @Test func noticeAfterARecapJoinsTheWork() {
        let items = [
            item(1, .text, role: "user", text: "go"), item(2, .text, text: "done"),
            item(3, .recap, role: "system", text: "recap"),
            item(4, .notice, role: "system", text: "Background command \"x\" completed (exit code 0)"),
            item(5, .text, text: "the command finished"),
        ]
        let blocks = ChatBlock.build(ChatStore.buildRows(items))
        #expect(blocks.count == 3) // turn, recap, a turn holding the notice in its work
        guard case .turn(let t) = blocks[2] else { Issue.record("turn expected"); return }
        #expect(t.users.isEmpty && t.process.count == 1)
        if case .assistant(let a) = t.answer { #expect(a.text == "the command finished") } else { Issue.record("answer") }
    }

    @Test func backgroundWakeKeepsTheSubstantialAnswer() {
        var wake = item(4, .notice, role: "system", text: "Background command \"Release\" completed (exit code 0)")
        wake.wake = true
        let summary = "All four are done and pushed; here is what changed and how to try each of them on the phone."

        // A full summary, then a short follow-up once the build is up: the summary is the answer, the follow-up
        // waits in the drawer.
        var items = [
            item(1, .text, role: "user", text: "go"), item(2, .toolUse, tool: "Bash", id: "a"),
            item(3, .text, text: summary),
            wake, item(5, .toolUse, tool: "Bash", id: "b"), item(6, .text, text: "build 42 is up"),
        ]
        var blocks = ChatBlock.build(ChatStore.buildRows(items))
        #expect(blocks.count == 1) // one exchange, not split at the wake
        guard case .turn(let a) = blocks[0] else { Issue.record("turn"); return }
        if case .assistant(let i) = a.answer { #expect(i.text == summary) } else { Issue.record("answer") }
        #expect(a.wakes.map(\.id) == [wake.id])
        #expect(a.tail.count == 3) // the wake, the follow-up tool and words: only in the drawer
        #expect(a.ordered.last?.id == "3#0")
        #expect(a.afterWake.first?.id == wake.id && a.afterWake.count == 3)

        // "Waiting for it", then the summary after the wake: the summary is the answer, and every tool before it
        // (the wake included) is one stretch of work.
        items = [
            item(1, .text, role: "user", text: "go"), item(2, .toolUse, tool: "Bash", id: "a"),
            item(3, .text, text: "waiting for the build"),
            wake, item(5, .toolUse, tool: "Bash", id: "b"), item(6, .text, text: summary),
        ]
        blocks = ChatBlock.build(ChatStore.buildRows(items))
        guard case .turn(let b) = blocks[0], blocks.count == 1 else { Issue.record("turn"); return }
        if case .assistant(let i) = b.answer { #expect(i.text == summary) } else { Issue.record("answer") }
        #expect(b.tail.isEmpty && b.process.count == 4) // tool, interim words, wake, tool

        // Absorbed mid-turn (no wake): an ordinary turn, the last words are the answer.
        wake.wake = nil
        blocks = ChatBlock.build(ChatStore.buildRows(items.map { $0.seq == 4 ? wake : $0 }))
        guard case .turn(let c) = blocks[0] else { Issue.record("turn"); return }
        #expect(c.wakes.isEmpty)
    }

    @Test func wakeAfterARecapStaysInItsExchange() {
        var wake = item(4, .notice, role: "system", text: "Background command \"Release\" completed (exit code 0)")
        wake.wake = true
        let summary = "Agent tags and XML handling are done and pushed; the build is still packaging."
        let items = [
            item(1, .text, role: "user", text: "go"), item(2, .text, text: summary),
            item(3, .recap, role: "system", text: "recap"),
            wake, item(5, .toolUse, tool: "Bash", id: "b"), item(6, .text, text: "build 42 is up"),
        ]
        let blocks = ChatBlock.build(ChatStore.buildRows(items))
        #expect(blocks.count == 2) // the exchange (wake folded in), then the recap card where it was
        guard case .turn(let t) = blocks[0], case .row(.recap) = blocks[1] else { Issue.record("shape \(blocks)"); return }
        if case .assistant(let a) = t.answer { #expect(a.text == summary) } else { Issue.record("answer") }
        #expect(t.wakes.count == 1 && t.tail.count == 3)
    }

    @Test func catchUpNeverEndsTheHistory() {
        let store = ChatStore(key: SessionKey(machineID: "test", sessionID: UUID().uuidString))
        store.merge((100...110).map { item($0, .text, text: "m\($0)") }, backlog: true, hasMore: true)
        #expect(store.hasMore)
        // A catch-up after since_seq (only newer items) saying "nothing older": not about our start, ignored.
        store.merge((111...112).map { item($0, .text, text: "m\($0)") }, backlog: true, hasMore: false)
        #expect(store.hasMore)
        // No answer at all (newer hubs omit it on catch-ups): unchanged.
        store.merge([item(113, .text, text: "m")], backlog: true, hasMore: Bool?.none)
        #expect(store.hasMore)
    }

    @Test func wakeSummaryReadsInChinese() {
        #expect(NoticeRow.wakeSummary("Background command \"Release master\" completed (exit code 0)") == ("后台命令完成 · Release master", false))
        #expect(NoticeRow.wakeSummary("Background command \"Build\" failed with exit code 65").failed)
        #expect(NoticeRow.wakeSummary("Agent \"Explore\" completed").text == "子 Agent 完成 · Explore")
        #expect(NoticeRow.wakeSummary("后台任务有更新").text == "后台任务有更新")
    }

    @Test func pendingMatchesTheTranscriptCopy() {
        #expect(PendingMessage.same("[Image #21]1 多了一个空格色块\n2 有的 agent", "1 多了一个空格色块 2 有的 agent"))
        #expect(PendingMessage.same("看这个", "看这个\n\n<attached_images>\n/a/1.jpg\n</attached_images>"))
        #expect(!PendingMessage.same("别的话", "看这个"))
        #expect(UserBubble.display("[Image #21]1 多了一个色块") == "1 多了一个色块")
    }

    @Test func contextTagRoundTrips() {
        let ref = ContextRef(kind: "edit", title: "A \"quoted\" <title>", url: "https://x.y/a?b=1&c=2", text: "选中的 <b>文字</b>")
        let msg = ref.xml + "\n\n按要求修改上面选中的内容：更短"
        let parsed = ContextRef.parse(msg)
        #expect(parsed.refs.count == 1)
        #expect(parsed.refs[0].kind == "edit" && parsed.refs[0].title == ref.title && parsed.refs[0].url == ref.url && parsed.refs[0].text == ref.text)
        #expect(parsed.rest == "按要求修改上面选中的内容：更短")
        #expect(ContextRef.parse("没有标签").refs.isEmpty)
    }
}

