import Foundation
import Testing
@testable import Persona

struct PermissionInputTests {
    @Test func secureFieldsRoundTripAndDisableSiriApproval() throws {
        let raw = Data(#"{"id":"ix_1","machine_id":"server","session_id":"s1","kind":"permission","fields":[{"key":"password","label":"密码","secret":true}]}"#.utf8)
        let ix = try JSONDecoder().decode(Interaction.self, from: raw)
        #expect(ix.requiresInput)
        #expect(ix.fields?.first?.secret == true)
        #expect(ApproveFirstIntent.resolution(for: ix) == nil)
        let resolution = Resolution(allow: true, values: ["password": "example-password"])
        let decoded = try JSONDecoder().decode(Resolution.self, from: JSONEncoder().encode(resolution))
        #expect(decoded.values?["password"] == "example-password")
    }
}

@MainActor
struct PermissionAlwaysScopeTests {
    @Test func bashNamesEveryCommandAndDirectoryRulesNameTheirScope() throws {
        let raw = Data(#"{"id":"ix_scope","machine_id":"computer","session_id":"chat","kind":"permission","tool_name":"Bash","always_keys":["tool:Bash"]}"#.utf8)
        var ix = try JSONDecoder().decode(Interaction.self, from: raw)
        #expect(ix.alwaysScopeDetail == String(localized: "All commands in this conversation"))
        let decoded = try JSONDecoder().decode(Interaction.self, from: JSONEncoder().encode(ix))
        #expect(decoded.alwaysKeys == ["tool:Bash"])

        ix.toolName = "Read"
        ix.alwaysKeys = ["tool:Read(//workspace/docs/**)"]
        #expect(ix.alwaysScopeDetail == String(localized: "All reads in this directory in this conversation"))
        for tool in ["Edit", "Write", "MultiEdit", "NotebookEdit"] {
            ix.toolName = tool
            ix.alwaysKeys = ["tool:Edit(//workspace/docs/**)"]
            #expect(ix.alwaysScopeDetail == String(localized: "All file edits in this directory in this conversation"))
        }

        ix.alwaysKeys = nil
        #expect(ix.alwaysScopeDetail == String(localized: "All uses of this tool in this conversation"))
        ix.alwaysKeys = ["lark:POST /conversations/*/messages"]
        #expect(ix.alwaysScopeDetail == String(localized: "The approved scope remains allowed in this conversation"))
    }

    @Test func anotherAgentsNarrowScopeIsPreserved() {
        var ix = Interaction(id: "ix_scope", machineID: "computer", sessionID: "chat", kind: .permission,
                             toolName: "Bash", agent: "codex", alwaysLabel: "Only this command")
        #expect(ix.alwaysScopeDetail == "Only this command")
        ix.alwaysLabel = nil
        #expect(ix.alwaysScopeDetail == nil)
    }
}
