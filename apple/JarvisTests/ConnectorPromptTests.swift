import Foundation
import Testing
@testable import Jarvis

/// A sign-in step as the hub sends it: fields to fill in, or a code to enter on a web page (GitHub's device flow).
struct ConnectorPromptTests {
    private func prompt(_ json: String) throws -> ConnectorPrompt {
        try JSON.decoder.decode(ConnectorPrompt.self, from: Data(json.utf8))
    }

    @Test func aDeviceCode() throws {
        let p = try prompt(#"{"step":"device","kind":"device_code","url":"https://github.com/login/device","user_code":"ABCD-1234","interval":5}"#)
        #expect(p.isDeviceCode)
        #expect(p.userCode == "ABCD-1234")
        #expect(p.interval == 5)
    }

    @Test func fieldsAsBefore() throws {
        let p = try prompt(#"{"step":"phone","fields":[{"name":"phone","label":"手机号"}]}"#)
        #expect(!p.isDeviceCode)
        #expect(p.fields?.first?.name == "phone")
    }
}
