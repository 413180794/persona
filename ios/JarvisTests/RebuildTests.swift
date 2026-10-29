import Foundation
import Testing
@testable import Jarvis

@MainActor
struct RebuildTests {
    /// A small deterministic generator (the same sequences every run).
    struct LCG {
        var state: UInt64
        mutating func next(_ n: Int) -> Int {
            state = state &* 6364136223846793005 &+ 1442695040888963407
            return Int((state >> 33) % UInt64(n))
        }
    }

    static func item(_ seq: Int, _ role: String, _ kind: ItemKind, text: String? = nil, tool: String? = nil, queued: Bool? = nil,
                     wake: Bool? = nil) -> Item {
        Item(id: "i\(seq)#0", seq: seq, ts: nil, role: role, kind: kind, text: text, toolUseID: tool, toolName: tool == nil ? nil : "Bash",
             toolInput: nil, isError: nil, fullSize: nil, image: nil, queued: queued, from: nil, wake: wake)
    }

    /// Items as a session writes them: prompts, queued input, thoughts, tool calls whose results sometimes come after the
    /// next prompt, notices (wakes, interruptions), recaps.
    static func session(_ seed: UInt64, count: Int) -> [Item] {
        var r = LCG(state: seed)
        var out: [Item] = []
        var open: [String] = []
        var seq = 0
        while out.count < count {
            seq += 1
            switch r.next(12) {
            case 0, 1: out.append(item(seq, "user", .text, text: "问 \(seq)"))
            case 2: out.append(item(seq, "user", .text, text: "插一句", queued: true))
            case 3: out.append(item(seq, "assistant", .thinking, text: "想"))
            case 4, 5:
                let t = "t\(seq)"
                out.append(item(seq, "assistant", .toolUse, tool: t))
                open.append(t)
            case 6, 7:
                if !open.isEmpty { out.append(item(seq, "user", .toolResult, text: "ok", tool: open.remove(at: r.next(open.count)))) }
            case 8: out.append(item(seq, "assistant", .text, text: "答 \(seq)"))
            case 9: out.append(item(seq, "system", .notice, text: r.next(2) == 0 ? "已中断" : "后台任务完成", wake: r.next(2) == 0))
            case 10: out.append(item(seq, "system", .recap, text: "回顾"))
            default: out.append(item(seq, "assistant", .text, text: "说明"))
            }
        }
        return out
    }

    @Test func incrementalRebuildEqualsAFullOne() {
        for seed in UInt64(1)...40 {
            let all = Self.session(seed, count: 160)
            var settled: ChatStore.SettledRows?
            var r = LCG(state: seed)
            var n = 0
            while n < all.count {
                n = min(all.count, n + 1 + r.next(4)) // events arrive one at a time or in small bursts
                let items = Array(all[..<n])
                let built = ChatStore.rebuild(items, settled: settled)
                let rows = ChatStore.buildRows(items)
                #expect(built.rows == rows, "seed \(seed), \(n) items: rows")
                #expect(built.blocks == ChatBlock.build(rows), "seed \(seed), \(n) items: blocks")
                if built.rows != rows { return }
                settled = built.settled
            }
            // An older page arrives in front: what was settled no longer holds.
            let older = Self.session(seed + 1000, count: 30).map { var it = $0; it.id = "old-" + it.id; it.toolUseID = it.toolUseID.map { "old-" + $0 }; return it }
            let items = older + all
            let built = ChatStore.rebuild(items, settled: settled)
            let rows = ChatStore.buildRows(items)
            #expect(built.rows == rows && built.blocks == ChatBlock.build(rows), "seed \(seed): after an older page")
        }
    }

    /// Cost of one event in a 1000-exchange session (printed, for the record): the full rebuild against the incremental one.
    @Test func incrementalRebuildIsCheap() {
        var items: [Item] = []
        var seq = 0
        for t in 0..<1000 {
            seq += 1; items.append(Self.item(seq, "user", .text, text: "第 \(t) 轮"))
            for _ in 0..<6 {
                seq += 1; items.append(Self.item(seq, "assistant", .toolUse, tool: "t\(seq)"))
                seq += 1; items.append(Self.item(seq, "user", .toolResult, text: "ok", tool: "t\(seq - 1)"))
            }
            seq += 1; items.append(Self.item(seq, "assistant", .text, text: "答"))
        }
        let settled = ChatStore.rebuild(items, settled: nil).settled
        seq += 1; items.append(Self.item(seq, "assistant", .thinking, text: "又想"))
        let clock = ContinuousClock()
        let full = clock.measure { _ = ChatBlock.build(ChatStore.buildRows(items)) }
        var built: (rows: [ChatRow], blocks: [ChatBlock], settled: ChatStore.SettledRows?)?
        let incremental = clock.measure { built = ChatStore.rebuild(items, settled: settled) }
        print("REBUILD-COST \(items.count) items: full \(full), incremental \(incremental)")
        #expect(built?.rows == ChatStore.buildRows(items))
    }
}
