import Testing
@testable import PersonaKit

/// /loop in an agent's composer (#2601): the same cases as the web's web/src/lib/loop-command.test.ts.
struct LoopCommandTests {
    @Test func knowsALoopDraftFromOtherWords() {
        #expect(LoopCommand.isDraft("/loop"))
        #expect(LoopCommand.isDraft("/loop "))
        #expect(LoopCommand.isDraft("/LOOP 5m x"))
        #expect(LoopCommand.isDraft("/loop\n看下 CI"))
        #expect(!LoopCommand.isDraft("/looping"))
        #expect(!LoopCommand.isDraft("看下 /loop"))
    }

    @Test func saysWhatIsMissing() {
        let usage = LoopCommand.hint("/loop ")
        #expect(usage?.missing == true)
        #expect(usage?.text.contains("/loop 10m 看下 CI") == true)
        #expect(LoopCommand.hint("/loop") == usage)
        #expect(LoopCommand.hint("/loop 10m") == LoopHint(text: "每 10 分钟：还没写要做的事", missing: true))
    }

    @Test func saysWhatItWillRepeatAndHowOften() {
        #expect(LoopCommand.hint("/loop 10m 看下 CI") == LoopHint(text: "每 10 分钟重复：看下 CI", missing: false))
        #expect(LoopCommand.hint("/loop 2H  跑一遍测试") == LoopHint(text: "每 2 小时重复：跑一遍测试", missing: false))
        #expect(LoopCommand.hint("/loop 30s ping")?.text == "每 30 秒重复：ping")
        #expect(LoopCommand.hint("/loop 1d 写日报")?.text == "每 1 天重复：写日报")
        #expect(LoopCommand.hint("/loop 看下 CI 有没有过") == LoopHint(text: "由 Claude 自己定节奏重复：看下 CI 有没有过", missing: false))
        #expect(LoopCommand.hint("/loop 10x 看下")?.text == "由 Claude 自己定节奏重复：10x 看下") // not an interval
        #expect(LoopCommand.hint("/loop +5m 看下")?.text == "由 Claude 自己定节奏重复：+5m 看下")
    }

    @Test func leavesOtherDraftsAlone() {
        #expect(LoopCommand.hint("继续") == nil)
        #expect(LoopCommand.hint("/compact") == nil)
        #expect(LoopCommand.hint("") == nil)
    }

    @Test func isOfferedToClaudeAgentsOnly() {
        #expect(LoopCommand.offered(agent: nil, sourceKind: nil, assistant: false))
        #expect(LoopCommand.offered(agent: "", sourceKind: nil, assistant: false))
        #expect(LoopCommand.offered(agent: "claude", sourceKind: nil, assistant: false))
        #expect(!LoopCommand.offered(agent: "codex", sourceKind: nil, assistant: false))
        #expect(!LoopCommand.offered(agent: "traex", sourceKind: nil, assistant: false))
        #expect(!LoopCommand.offered(agent: nil, sourceKind: "codex_app_server", assistant: false))
        #expect(!LoopCommand.offered(agent: "claude", sourceKind: nil, assistant: true)) // the web's assistant has no commands
    }
}
