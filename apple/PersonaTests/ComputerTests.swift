import Foundation
import Testing
@testable import Jarvis

/// Operating a Mac through Cua Driver (the hub's plugin cua, docs/computer-use.md): what the machine list says of it,
/// and how the assistant's two tools read in the chat.
@MainActor
struct ComputerTests {
    private func machine(_ extra: String = "") throws -> Machine {
        try JSON.decoder.decode(Machine.self, from: Data("{\"id\":\"macbook\",\"name\":\"MacBook\",\"kind\":\"ssh\",\"online\":true\(extra)}".utf8))
    }

    @Test func theMachineListSaysItOnlyWhileThePluginIsOn() throws {
        #expect(try machine().computer == nil, "plugin off, an older hub, not a Mac")
        let off = try machine(",\"computer\":{\"enabled\":false}")
        #expect(off.computer == MachineComputer(enabled: false))
        let c = try #require(try machine(",\"computer\":{\"enabled\":true,\"installed\":true,\"version\":\"0.38.0\",\"daemon\":true,"
            + "\"accessibility\":\"granted\",\"screen_recording\":\"missing\",\"desktop\":\"active\",\"ready\":false,\"note\":\"还没有屏幕录制权限\"}").computer)
        #expect(c.enabled && !c.ready && c.screenRecording == "missing" && c.note == "还没有屏幕录制权限")
        let m = try machine(",\"computer\":{\"enabled\":true,\"ready\":true}")
        #expect(try JSON.decoder.decode(Machine.self, from: JSON.encoder.encode(m)) == m)
    }

    private func call(_ name: String, _ input: [String: JSONValue]) -> ToolCall {
        let use = Item(id: "1#0", seq: 1, ts: Date(timeIntervalSince1970: 1), role: "assistant", kind: .toolUse, text: nil,
                       toolUseID: "t1", toolName: "mcp__jarvis__" + name, toolInput: .object(input), isError: nil, image: nil)
        return ToolCall(use: use, result: nil)
    }

    @Test func aToolThatOperatesAMacShowsItsScreen() throws {
        let mac = try machine()
        var work = try machine()
        work.group = "work"
        let machines = [mac, work]
        let named: JSONValue = .object(["machine": .string("MacBook")])
        #expect(ScreenEntry.machine("mcp__jarvis__computer_run", input: named, chat: MachineRef(id: "server"), machines: machines) == mac.ref,
                "the assistant's: the Mac it names, by name too")
        #expect(ScreenEntry.machine("mcp__jarvis__computer_state", input: .object(["machine": .string("macbook")]),
                                    chat: MachineRef(group: "work", id: "server"), machines: machines) == MachineRef(group: "work", id: "macbook"),
                "in the chat's group of hubs")
        #expect(ScreenEntry.machine("mcp__cua-driver__click", input: .object([:]), chat: mac.ref, machines: machines) == mac.ref,
                "an agent's own Cua Driver: its machine")
        #expect(ScreenEntry.machine("Bash", input: named, chat: mac.ref, machines: machines) == nil)
        #expect(ScreenEntry.machine("mcp__jarvis__computer_run", input: .object([:]), chat: mac.ref, machines: machines) == nil)
    }

    @Test func theMachineListSaysWhetherItsScreenCanBeShown() throws {
        #expect(try machine(",\"computer\":{\"enabled\":true,\"screen_sharing\":true}").computer?.screenSharing == true)
        #expect(try machine(",\"computer\":{\"enabled\":true}").computer?.screenSharing == false, "an older hub")
    }

    /// #1280: one step per missing permission in System Settings' own words, the allowed ones checked first.
    @Test func theGrantGuideSaysWhatIsLeft() {
        let both = GrantGuide.steps(MachineComputer(enabled: true, installed: true, daemon: true, accessibility: "missing", screenRecording: "missing"))
        #expect(both.map(\.id) == ["accessibility", "screen", "prompt", "recheck"])
        #expect(both[0].text.contains("系统设置 › 隐私与安全性 › 辅助功能") && both[0].text.contains("「Cua Driver」"))
        #expect(both[1].text.hasPrefix("同一处的「录屏与系统录音」"))
        #expect(GrantGuide.number(of: both[3], in: both) == 4)
        #expect(GrantGuide.header(MachineComputer(accessibility: "missing", screenRecording: "missing"), asking: false) == "Cua Driver 还需要两项权限")

        let screen = GrantGuide.steps(MachineComputer(enabled: true, installed: true, daemon: true, accessibility: "granted", screenRecording: "missing"))
        #expect(screen.map(\.id) == ["accessibility", "screen", "prompt", "recheck"] && screen[0].done && !screen[1].done)
        #expect(screen[0].text == "辅助功能：已允许 Cua Driver")
        #expect(screen[1].text.contains("系统设置 › 隐私与安全性 › 录屏与系统录音") && screen[1].text.contains("屏幕录制"))
        #expect(GrantGuide.number(of: screen[1], in: screen) == 1, "the allowed one has no number")
        #expect(GrantGuide.header(MachineComputer(accessibility: "granted", screenRecording: "missing"), asking: false) == "Cua Driver 还需要一项权限")

        let accessibility = GrantGuide.steps(MachineComputer(accessibility: "missing", screenRecording: "granted"))
        #expect(accessibility.map(\.id) == ["screen", "accessibility", "recheck"], "no prompt step without screen recording")
    }

    @Test func theToolsReadInChinese() {
        let look = call("computer_state", ["machine": .string("macbook"), "app": .string("Calculator")])
        #expect(ToolPresentation.verb(look.name) == "看电脑")
        #expect(ToolPresentation.subject(look) == "macbook · Calculator")
        #expect(ToolPresentation.subject(call("computer_state", ["machine": .string("macbook")])) == "macbook · 整个屏幕")
        let steps = call("computer_run", ["machine": .string("macbook"), "steps": .array([.object([:]), .object([:])])])
        #expect(ToolPresentation.verb(steps.name) == "操控电脑")
        #expect(ToolPresentation.subject(steps) == "macbook · 2 步")
        let script = call("computer_run", ["machine": .string("macbook"), "script": .string("const app = await cua.getApp(\"Calculator\");\nreturn 1")])
        #expect(ToolPresentation.subject(script) == "macbook · const app = await cua.getApp(\"Calculator\");")
        #expect(ToolPresentation.symbol(steps.name) == Theme.Icon.machine)
    }
}
