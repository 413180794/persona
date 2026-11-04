import Foundation
import Testing
@testable import Jarvis

/// A sign-in step as the hub sends it: fields to fill in, a code to enter on a web page (GitHub's device flow), or a QR
/// code to scan with another app (WeChat).
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
        #expect(!p.isQRCode)
        #expect(!p.polls)
        #expect(p.fields?.first?.name == "phone")
    }

    @Test func aQRCode() throws {
        let p = try prompt(#"{"step":"qr","kind":"qr","url":"https://example.com/q","interval":2,"qr":{"size":2,"rows":["10","01"]}}"#)
        #expect(p.isQRCode)
        #expect(!p.isDeviceCode)
        #expect(p.polls)
        #expect(p.qr?.rows == ["10", "01"])
        #expect(p.pollKey == "1001")
    }

    /// A QR step from a hub that did not draw the code (no `qr`) is not shown as one: nothing to scan.
    @Test func aQRStepWithoutTheCode() throws {
        let p = try prompt(#"{"step":"qr","kind":"qr","url":"https://example.com/q","interval":2}"#)
        #expect(!p.isQRCode)
        #expect(!p.polls)
    }

    /// A new code (it expired and the connector made another) restarts the polling.
    @Test func aNewCodeIsANewPoll() throws {
        let a = try prompt(#"{"step":"qr","kind":"qr","qr":{"size":1,"rows":["1"]}}"#)
        let b = try prompt(#"{"step":"qr","kind":"qr","qr":{"size":1,"rows":["0"]}}"#)
        #expect(a.pollKey != b.pollKey)
    }
}
