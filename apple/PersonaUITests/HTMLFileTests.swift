import XCTest

/// An .html file previews isolated (#1297): scripts and the network off until the ⋯ menu turns them on.
///   PORT=18785 MOCK_AGENTS=1 MOCK_OFFICE=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18785 xcodebuild test … -only-testing:PersonaUITests/HTMLFileTests
final class HTMLFileTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_AGENTS=1 MOCK_OFFICE=1 (PERSONA_MOCK_HUB)")
        }
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "html \(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor func testIsolatedPreview() {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000001",
                               "-openFilePath", "docs/报告.html"]
        app.launchPinned()
        XCTAssertTrue(app.descendants(matching: .any)["file-html"].waitForExistence(timeout: 25), "the page did not open")
        let page = app.webViews.firstMatch
        let shown = page.staticTexts["本周降雨"].waitForExistence(timeout: 10)
        shot("first")
        XCTAssertTrue(shown, "the page's text is not there:\n\(app.debugDescription.prefix(3000))")
        XCTAssertFalse(page.staticTexts["脚本运行了"].exists, "a script ran with scripts off")
        let read = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "读入了同目录的 2 个文件")).firstMatch
        XCTAssertTrue(read.waitForExistence(timeout: 5), "the style sheet and the picture beside the page were not read in")
        sleep(1)
        shot("off")
        app.buttons["更多"].firstMatch.tap()
        let scripts = app.switches["允许运行脚本"].firstMatch.exists ? app.switches["允许运行脚本"].firstMatch : app.buttons["允许运行脚本"].firstMatch
        XCTAssertTrue(scripts.waitForExistence(timeout: 5), "no 允许运行脚本 in the menu")
        scripts.tap()
        XCTAssertTrue(app.webViews.firstMatch.staticTexts["脚本运行了"].waitForExistence(timeout: 10), "the script did not run once allowed")
        sleep(1)
        shot("scripts")
    }
}
