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
