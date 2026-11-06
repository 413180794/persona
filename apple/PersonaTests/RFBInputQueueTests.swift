import Testing
@testable import Persona

/// Keys and pastes to the Mac's Screen Sharing in the order they were made (#2374, RFBInputQueue): a paste's clipboard
/// write takes a while (the hub), and nothing made after it may overtake its ⌘V; a failed write sends no ⌘V.
@MainActor struct RFBInputQueueTests {
    /// What went out, as text: a key down as its character (⌘V as "⌘V"), key ups left out; clipboard writes as [text]
    /// when they are answered. Writes wait until `answer()`, one at a time in the order made.
    @MainActor final class Wire {
        var out: [String] = []
        /// Writes that fail (by their text).
        var failing: Set<String> = []
        /// Writes started and not answered yet: more than one means two pastes overlap.
        private(set) var waiting: [(String, CheckedContinuation<Bool, Never>)] = []
        private(set) var mostWaiting = 0
        private var command = false

        lazy var queue = RFBInputQueue(send: { [unowned self] e in self.sent(e) }, write: { [unowned self] text in
            await withCheckedContinuation { k in
                self.waiting.append((text, k))
                self.mostWaiting = max(self.mostWaiting, self.waiting.count)
            }
        })

        private func sent(_ e: RFBInputQueue.Event) {
            if e.keysym == RFBSession.command { command = e.down; return }
            guard e.down else { return }
            if command, e.keysym == 0x76 { out.append("⌘V"); return }
            out.append(String(Character(Unicode.Scalar(e.keysym)!)))
        }

        /// Lets the queue run until a write is waiting (or nothing is left), then answers the oldest.
        func answer() async {
            for _ in 0..<1000 {
                if !waiting.isEmpty || queue.idle { break }
                await Task.yield()
            }
            // A few more turns: a second write started meanwhile (overlap) shows up in mostWaiting.
            for _ in 0..<20 { await Task.yield() }
            guard !waiting.isEmpty else { return }
            let (text, k) = waiting.removeFirst()
            out.append("[\(text)]")
            k.resume(returning: !failing.contains(text))
        }

        func drain() async {
            for _ in 0..<100 where !queue.idle { await answer() }
        }
    }

    @Test func keysGoOutAtOnceWithNothingPending() {
        let w = Wire()
        w.queue.type("ab") {}
        #expect(w.out == ["a", "b"])
        #expect(w.queue.idle)
    }

    /// 「a你b」: b waits for 「你」's ⌘V instead of going out before it; so does a key pressed after it.
    @Test func typedTextGoesOutInOrder() async {
        let w = Wire()
        w.queue.type("a你b") {}
        w.queue.press(0x63)
        #expect(w.out == ["a"])
        await w.drain()
        #expect(w.out == ["a", "[你]", "⌘V", "b", "c"])
        #expect(w.queue.idle)
    }

    /// 「你好」 then 「，」: the second write waits for the first one's ⌘V (no pasting 「，」 twice, no swapping).
    @Test func pastesInARowDoNotOverlap() async {
        let w = Wire()
        w.queue.type("你好") {}
        w.queue.type("，") {}
        let third = w.queue.paste("吗")
        await w.drain()
        #expect(w.out == ["[你好]", "⌘V", "[，]", "⌘V", "[吗]", "⌘V"])
        #expect(w.mostWaiting == 1)
        #expect(await third.value)
    }

    /// A failed write: no ⌘V (it would paste the Mac's old clipboard), what follows still goes out, and done says so
    /// before it does.
    @Test func aFailedWriteSendsNoCommandV() async {
        let w = Wire()
        w.failing = ["你"]
        let failed = w.queue.paste("你") { ok in w.out.append(ok ? "ok" : "failed") }
        w.queue.type(" 好") {}
        await w.drain()
        #expect(w.out == ["[你]", "failed", " ", "[好]", "⌘V"])
        #expect(await failed.value == false)
    }

    /// A run of failed pastes in typed text tells once; a paste that goes through ends the run, and so does
    /// everything having gone out before the next text.
    @Test func aRunOfFailuresTellsOnce() async {
        let w = Wire()
        w.failing = ["你", "好", "吗"]
        var told = 0
        w.queue.type("你a好") { told += 1 }
        w.queue.type("b吗") { told += 1 }
        await w.drain()
        #expect(w.out == ["[你]", "a", "[好]", "b", "[吗]"])
        #expect(told == 1)

        w.queue.type("你") { told += 1 }
        await w.drain()
        #expect(told == 2)

        w.queue.type("你x吗") { told += 1 }
        w.queue.type("好") { told += 1 }
        await w.drain()
        #expect(told == 3)
    }

    @Test func aPasteThroughEndsTheRun() async {
        let w = Wire()
        w.failing = ["你", "吗"]
        var told = 0
        w.queue.type("你a世b吗") { told += 1 }
        await w.drain()
        #expect(w.out == ["[你]", "a", "[世]", "⌘V", "b", "[吗]"])
        #expect(told == 2)
    }

    /// Once all of it has gone out, keys go out at once again.
    @Test func idleAgainAfterThePasteGoesOut() async {
        let w = Wire()
        w.queue.paste("你")
        await w.drain()
        w.queue.press(0x78)
        #expect(w.out == ["[你]", "⌘V", "x"])
    }
}
