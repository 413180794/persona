import Foundation
import Testing
@testable import Persona

/// A topic group's cards show their topics' newest replies (`LarkTopicReplies`, #1087): which replies and how many
/// earlier ones (the Lark gateway's topic-reply-preview cases), what a reply's line says (Message Tweaks, encryption), at most three
/// reads at a time, read again only when a reply count changes (also during a read) or Retry asks.
@MainActor struct LarkTopicRepliesTests {
    private func msg(_ id: String, replies: Int = 0, threadReplies: Int? = nil) -> LarkMessage {
        LarkMessage(id: id, position: id, senderID: nil, senderName: "x", isMine: false, type: "TEXT", text: id, createdAt: nil,
                    recalled: false, replyCount: replies, threadReplyCount: threadReplies)
    }

    /// The reads a test's store makes and how many are under way; while `held`, a read waits for `release()`.
    @MainActor final class Probe {
        var calls: [String] = []
        var running = 0, most = 0
        var failing: Set<String> = []
        var held = false
        private var gates: [CheckedContinuation<Void, Never>] = []

        func pass() async {
            guard held else { return }
            await withCheckedContinuation { gates.append($0) }
        }

        /// Lets the reads under way finish.
        func release() {
            let g = gates
            gates = []
            g.forEach { $0.resume() }
        }
    }

    private func fetch(_ probe: Probe) -> (String, Int) async throws -> [LarkMessage] {
        { id, limit in
            probe.calls.append(id)
            probe.running += 1
            probe.most = max(probe.most, probe.running)
            defer { probe.running -= 1 }
            await probe.pass()
            if probe.failing.contains(id) { throw URLError(.timedOut) }
            #expect(limit == LarkTopicReplies.shown + 1)
            return [msg(id)] + (1...limit).map { msg("\(id)-r\($0)") }
        }
    }

    private func store(_ probe: Probe) -> LarkTopicReplies {
        let s = LarkTopicReplies()
        s.fetch = fetch(probe)
        return s
    }

    /// Until `done`, at most two seconds.
    private func settle(_ done: () -> Bool) async {
        for _ in 0..<200 where !done() { try? await Task.sleep(for: .milliseconds(10)) }
    }

    /// Long enough for a read that should not happen to have happened.
    private func pause() async { try? await Task.sleep(for: .milliseconds(150)) }

    @Test func thePreviewIsTheNewestTwoRepliesWithoutTheTopic() {
        var p = LarkTopicReplies.preview(of: [msg("root"), msg("r1"), msg("r2")], topic: "root", count: 2)
        #expect(p.replies.map(\.id) == ["r1", "r2"])
        #expect(p.earlier == 0, "no 查看更早 0 条回复")
        p = LarkTopicReplies.preview(of: [msg("root"), msg("r1"), msg("r2"), msg("r3")], topic: "root", count: 6)
        #expect(p.replies.map(\.id) == ["r2", "r3"])
        #expect(p.earlier == 4)
        p = LarkTopicReplies.preview(of: [msg("r")], topic: "root", count: 1)
        #expect(p.replies.map(\.id) == ["r"], "a page without the topic")
        #expect(p.earlier == 0)
        p = LarkTopicReplies.preview(of: [msg("root")], topic: "root", count: 0)
        #expect(p.replies.isEmpty && p.earlier == 0, "the topic alone is not a reply")
        #expect(LarkTopicReplies.preview(of: [msg("a"), msg("b")], topic: "root", count: 1).earlier == 0, "a count behind the page")
    }

    @Test func theCountIsTheLargerOfFeishusTwo() {
        #expect(LarkTopicReplies.count(of: msg("t", replies: 1, threadReplies: 2)) == 2)
        #expect(LarkTopicReplies.count(of: msg("t", replies: 3)) == 3)
        #expect(LarkTopicReplies.count(of: msg("t")) == 0)
    }

    /// A reply's line follows Message Tweaks as a chat's row does: a recalled one only with Keep Recalled Messages on and its content kept (and
    /// some content to show), marked Recalled; an encrypted one shows its cover, never its body.
    @Test func aReplysLineFollowsTheDisplaySettings() {
        let off = LarkMessageDisplay.hidden
        let on = LarkMessageDisplay(accountID: "a", antiRecall: true, showEditHistory: false)
        var kept = msg("留存的原文")
        kept.recalled = true
        kept.retained = true
        kept.parts = [LarkPart(kind: "text", text: "留存的原文")]
        #expect(LarkTopicReplies.line(of: kept, display: off) == "消息已撤回")
        #expect(LarkTopicReplies.line(of: kept, display: on) == "留存的原文 · 已撤回")
        var lost = kept
        lost.retained = false
        #expect(LarkTopicReplies.line(of: lost, display: on) == "消息已撤回")
        var bare = kept
        bare.parts = []
        #expect(LarkTopicReplies.line(of: bare, display: on) == "消息已撤回")
        var secret = msg("x")
        secret.olk = LarkEncryption(version: 1, state: "decrypted", publicParts: [LarkPart(kind: "text", text: "封面")],
                                    bodyParts: [LarkPart(kind: "text", text: "正文")])
        #expect(LarkTopicReplies.line(of: secret, display: on) == "封面")
        #expect(LarkTopicReplies.line(of: msg("平常的回复"), display: off) == "平常的回复")
    }

    @Test func atMostThreeTopicsAreReadAtATime() async {
        let probe = Probe()
        probe.held = true
        let s = store(probe)
        for i in 0..<8 { s.want(msg("t\(i)", replies: 5)) }
        await settle { probe.running == LarkTopicReplies.parallel }
        await pause()
        #expect(probe.calls == ["t0", "t1", "t2"], "three at a time, in the order the cards came on screen")
        // Each round, the reads under way finish and the next ones start.
        for _ in 0..<5 where s.previews.count < 8 {
            let before = s.previews.count
            probe.release()
            await settle { s.previews.count > before && probe.running == min(LarkTopicReplies.parallel, 8 - s.previews.count) }
            #expect(probe.running <= LarkTopicReplies.parallel)
        }
        #expect(s.previews.count == 8)
        #expect(probe.most == LarkTopicReplies.parallel)
        #expect(probe.calls == (0..<8).map { "t\($0)" })
        #expect(s.previews["t0"]?.replies.map(\.id) == ["t0-r2", "t0-r3"])
    }

    @Test func aTopicIsReadAgainOnlyWhenItsCountChanges() async {
        let probe = Probe()
        let s = store(probe)
        s.want(msg("t", replies: 2))
        await settle { s.previews["t"] != nil }
        s.want(msg("t", replies: 2))
        s.want(msg("t", replies: 2))
        await pause()
        #expect(probe.calls == ["t"], "the same count is not read again")
        s.want(msg("t", replies: 3))
        await settle { s.previews["t"]?.count == 3 }
        #expect(probe.calls == ["t", "t"])
        s.want(msg("none"))
        await pause()
        #expect(probe.calls == ["t", "t"], "a topic with no replies is not read")
    }

    @Test func aCountThatChangesDuringItsReadIsReadAgain() async {
        let probe = Probe()
        probe.held = true
        let s = store(probe)
        s.want(msg("t", replies: 2))
        await settle { probe.running == 1 }
        s.want(msg("t", replies: 3)) // a reply came while the read was under way
        probe.held = false
        probe.release()
        await settle { s.previews["t"]?.count == 3 }
        #expect(s.previews["t"]?.count == 3, "the card would show the replies read at 2 under a count of 3")
        #expect(probe.calls == ["t", "t"])
        // Back to the count being read: nothing more to read.
        probe.held = true
        s.want(msg("t", replies: 4))
        await settle { probe.running == 1 }
        s.want(msg("t", replies: 5))
        s.want(msg("t", replies: 4))
        probe.held = false
        probe.release()
        await settle { s.previews["t"]?.count == 4 }
        await pause()
        #expect(probe.calls.count == 3)
        // Off screen meanwhile: the newer count waits until the card is back.
        probe.held = true
        s.want(msg("t", replies: 6))
        await settle { probe.running == 1 }
        s.want(msg("t", replies: 7))
        s.drop("t")
        probe.held = false
        probe.release()
        await settle { s.previews["t"]?.count == 6 }
        await pause()
        #expect(probe.calls.count == 4)
        #expect(s.previews["t"]?.count == 6)
    }

    @Test func whatIsWantedBeforeTheHubIsReadOnceItIsThere() async {
        let probe = Probe()
        let s = LarkTopicReplies()
        s.want(msg("t", replies: 1))
        await pause()
        #expect(s.previews.isEmpty)
        s.fetch = fetch(probe)
        await settle { s.previews["t"] != nil }
        #expect(probe.calls == ["t"])
    }

    @Test func aCardOffScreenBeforeItsTurnIsNotRead() async {
        let probe = Probe()
        let s = store(probe)
        for i in 0..<5 { s.want(msg("t\(i)", replies: 1)) }
        s.drop("t4")
        await settle { s.previews.count == 4 }
        await pause()
        #expect(!probe.calls.contains("t4"))
        #expect(s.previews["t4"] == nil)
    }

    @Test func aFailedReadIsMarkedAndReadAgainOnRetry() async {
        let probe = Probe()
        probe.failing = ["t"]
        let s = store(probe)
        s.want(msg("t", replies: 1))
        await settle { s.previews["t"]?.failed == true }
        #expect(s.previews["t"]?.failed == true)
        #expect(s.previews["t"]?.replies.isEmpty == true)
        probe.failing = []
        s.retry(msg("t", replies: 1))
        await settle { s.previews["t"]?.replies.isEmpty == false }
        #expect(s.previews["t"]?.failed == false)
        #expect(s.previews["t"]?.replies.map(\.id) == ["t-r2", "t-r3"])
        #expect(probe.calls == ["t", "t"])
    }
}
