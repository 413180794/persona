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
            app.launchJarvis()
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

    /// The list column reaches the top of the screen (the strip under the status bar has its color), and when the
    /// column is closed the strip is the detail's again (2026-10-31: the list looked cut off at the top).
    @MainActor func testSidebarReachesTheTop() throws {
        XCUIDevice.shared.orientation = .portrait
        for style in ["glass", "claude"] {
            app.launchArguments += ["-ui.style", style]
            app.launchJarvis()
            XCTAssertTrue(app.staticTexts["选一个 Agent"].waitForExistence(timeout: 20))
            sleep(1)
            var image = app.screenshot().image
            shot("top-\(style)")
            let scale = image.scale
            func color(_ x: CGFloat, _ y: CGFloat) -> [Int] {
                guard let cg = image.cgImage?.cropping(to: CGRect(x: x * scale, y: y * scale, width: 1, height: 1)),
                      let data = cg.dataProvider?.data, let p = CFDataGetBytePtr(data) else { return [] }
                return [Int(p[0]), Int(p[1]), Int(p[2])]
            }
            // The same to the eye: the system's glass edge over the list's top tints it a few levels.
            func near(_ a: [Int], _ b: [Int]) -> Bool { a.count == 3 && b.count == 3 && zip(a, b).allSatisfy { abs($0 - $1) <= 8 } }
            // Above the list column (in the status bar), the same color as the column under it; the detail column's
            // strip stays the detail's.
            XCTAssertTrue(near(color(8, 2), color(8, 34)), "\(style): the strip above the list (\(color(8, 2))) is not the list's color (\(color(8, 34)))")
            let right = app.windows.firstMatch.frame.width - 40
            XCTAssertTrue(near(color(right, 2), color(right, 400)), "\(style): the strip above the detail changed")
            // Close the column: what is at the top there is the detail's color again, as below it.
            let toggle = app.buttons.matching(NSPredicate(format: "label IN %@", ["隐藏边栏", "Hide Sidebar", "边栏", "Sidebar"])).firstMatch
            XCTAssertTrue(toggle.exists, "no sidebar button:\n\(app.debugDescription)")
            toggle.tap()
            sleep(1)
            image = app.screenshot().image
            XCTAssertTrue(near(color(8, 2), color(8, 400)), "\(style): the list's color stayed at the top after closing it (\(color(8, 2)))")
            app.terminate()
            app.launchArguments.removeLast(2)
        }
    }
}
