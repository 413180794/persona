import XCTest

/// Pasting photos / files into a terminal the app drives (docs/integrations/terminal.md section 18「iOS / Mac」), against
/// scripts/mock/mock_hub.py with MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 (TEST_RUNNER_PERSONA_MOCK_HUB): holding the key
/// bar's paste offers Photos / Files, and each opens its picker.
final class TerminalFilesUITests: XCTestCase {
    private let widget = "a1f3c2d4-0000-4000-8000-000000000001"

    @MainActor func testPasteOffersPhotosAndFiles() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/" + widget, "-openTerminalPane", "YES",
                               "-terminalTipsSeen", "YES"]
        app.launchPinned()

        let paste = app.buttons["terminal-paste"]
        XCTAssertTrue(paste.waitForExistence(timeout: 20), "no paste key:\n\(app.debugDescription)")
        let size = app.descendants(matching: .any)["terminal-size"]
        XCTAssertTrue(size.waitForExistence(timeout: 15), "no size chip")
        let driving = NSPredicate(format: "label CONTAINS %@", "×")
        XCTAssertEqual(XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: driving, object: size)], timeout: 15), .completed,
                       "not driving the terminal: \(size.label)")
        paste.press(forDuration: 1.0)
        XCTAssertTrue(app.buttons["粘贴文字"].waitForExistence(timeout: 5), "no menu:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["照片"].exists)
        let files = app.buttons["文件"]
        XCTAssertTrue(files.exists)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("terminal-files-menu.png"))
        }
        files.tap()
        // The document picker is another process's view (its contents load late, if at all, on a simulator): that a
        // sheet now covers the key bar is enough to know it came up.
        let covered = XCTNSPredicateExpectation(predicate: NSPredicate(format: "isHittable == false"), object: paste)
        XCTAssertEqual(XCTWaiter.wait(for: [covered], timeout: 10), .completed, "no file picker:\n\(app.debugDescription)")
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("terminal-files-picker.png"))
        }
    }
}
