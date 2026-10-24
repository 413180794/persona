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
    }
}
