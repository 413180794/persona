import Foundation
import Testing
@testable import Jarvis

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
