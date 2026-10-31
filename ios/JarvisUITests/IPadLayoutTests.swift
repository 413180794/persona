import XCTest

/// iPad (docs/ios.md「iPad」): the app runs natively (not the phone-sized window), the lists sit in a sidebar wide
/// enough for their cards, in portrait and landscape. Against scripts/mock_hub.py (JARVIS_MOCK_HUB), on an iPad
/// simulator; skipped on iPhone. TEST_RUNNER_JARVIS_SHOTS=<dir> keeps a screenshot of each.
final class IPadLayoutTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard UIDevice.current.userInterfaceIdiom == .pad else { throw XCTSkip("iPad only") }
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-dev.openChat", "", "-openAgentsFresh", "YES"]
    }

    override func tearDown() {
        XCUIDevice.shared.orientation = .portrait
        super.tearDown()
    }

    @MainActor private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] else { return }
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("ipad-\(name).png"))
    }

    @MainActor func testFillsTheScreenWithASidebar() {
        for (name, orientation) in [("portrait", UIDeviceOrientation.portrait), ("landscape", .landscapeLeft)] {
            XCUIDevice.shared.orientation = orientation
            app.launch()
            let empty = app.staticTexts["选一个 Agent"]
            XCTAssertTrue(empty.waitForExistence(timeout: 20), "no detail column in \(name):\n\(app.debugDescription)")
            let window = app.windows.firstMatch.frame
            // Native: the window is the screen, not a phone-sized box in the middle.
            XCTAssertGreaterThan(window.width, 700, "\(name): window \(window)")
            // The list column is wider than the system's narrow sidebar, and the detail column keeps most of the rest.
            let search = app.searchFields.firstMatch
            XCTAssertTrue(search.waitForExistence(timeout: 5))
            XCTAssertGreaterThanOrEqual(search.frame.maxX, 330, "\(name): sidebar too narrow (\(search.frame))")
            XCTAssertGreaterThan(empty.frame.minX, search.frame.maxX, "\(name): the detail column is beside the list")
            shot("agents-\(name)")
            // A chat opens beside the list.
            app.buttons.matching(identifier: "agents.row").firstMatch.tap()
            XCTAssertTrue(empty.waitForNonExistence(timeout: 10), "\(name): the chat did not open in the detail column")
            XCTAssertTrue(search.exists, "\(name): the list went away when the chat opened")
            shot("chat-\(name)")
            // TEST_RUNNER_JARVIS_HOLD=<seconds>: stay on each page so `simctl io screenshot` can take it (XCUITest's own
            // screenshot comes out turned in landscape).
            if let hold = ProcessInfo.processInfo.environment["JARVIS_HOLD"].flatMap(UInt32.init) { sleep(hold) }
            app.terminate()
        }
    }
}
