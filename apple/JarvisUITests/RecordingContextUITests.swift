import XCTest

final class RecordingContextUITests: XCTestCase {
    @MainActor func testPickedRecordingStaysInDraftUntilSend() throws {
        try pickedRecording(oldLayout: false)
    }

    @MainActor func testOldLayoutPickedRecordingStaysInDraftUntilSend() throws {
        try pickedRecording(oldLayout: true)
    }

    @MainActor private func pickedRecording(oldLayout: Bool) throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else { throw XCTSkip("需要独立 mock hub") }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-ui.oldLayout", oldLayout ? "YES" : "NO", "-tab", "assistant", "-newAssistant", "YES",
                               "-openRecording", "YES", "-showRecordingSample", "YES"]
        app.launchPinned()
        let attach = app.buttons["recording-attach"]
        XCTAssertTrue(attach.waitForExistence(timeout: 15), app.debugDescription)
        let before = try sent(hub).count
        attach.tap()
        XCTAssertTrue(app.buttons["发送"].firstMatch.waitForExistence(timeout: 8))
        XCTAssertEqual(try sent(hub).count, before, "选为上下文不能自动发送")
        sleep(1)
        app.buttons["发送"].firstMatch.tapCenter()
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline, try sent(hub).count == before { usleep(100_000) }
        let text = try sent(hub).last ?? ""
        XCTAssertTrue(text.contains("<jarvis-context"), text)
        XCTAssertTrue(text.contains("[0:28]"), text)
        XCTAssertTrue(text.contains("下周整理一次进展"), text)
        XCTAssertFalse(text.contains(".m4a"), text)
    }

    private func sent(_ hub: String) throws -> [String] {
        let data = try Data(contentsOf: URL(string: hub + "/mock/sent")!)
        struct Output: Decodable { struct Message: Decodable { var text: String? }; var sent: [Message] }
        return try JSONDecoder().decode(Output.self, from: data).sent.compactMap(\.text)
    }
}
