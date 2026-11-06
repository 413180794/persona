import XCTest

/// The shared View snapshots of #1852, rendered through mock HTTP and the real app's pages; the snapshot data is not rewritten.
final class ExtensionShowcaseTests: XCTestCase {
    @MainActor func testEverySharedSnapshot() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["PERSONA_EXTENSIONS_HUB"], let dir = env["PERSONA_SHOTS"] else {
            throw XCTSkip("需要 MOCK_EXTENSIONS=1 和截图目录")
        }
        continueAfterFailure = false
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openExtension", "showcase", "-openExtensionPage", "snapshots"]
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for title in ["允许", "Allow"] where springboard.buttons[title].waitForExistence(timeout: 3) {
            springboard.buttons[title].tap()
        }
        let names = ["button", "card", "chart", "detail", "divider", "edit", "empty", "field", "home",
                     "invalidate-done", "invalidate-loading", "invalidate", "link", "list-filter", "list-more", "list-none",
                     "list-search", "list", "loading", "markdown", "metric", "progress", "row", "segmented-30d", "segmented",
                     "shortcut-count", "stat_grid", "table", "text", "widget-accessory_circular", "widget-accessory_inline",
                     "widget-accessory_rectangular", "widget-large", "widget-medium", "widget-small"]
        XCTAssertTrue(app.buttons["plugin-ui-action-snapshot-button"].waitForExistence(timeout: 15), app.debugDescription)
        for name in names.sorted() {
            let row = app.buttons["plugin-ui-action-snapshot-" + name]
            for _ in 0..<14 where !row.isHittable { app.swipeUp() }
            XCTAssertTrue(row.isHittable, "快照入口不可点：\(name)\n\(app.debugDescription)")
            row.tap()
            XCTAssertTrue(row.waitForNonExistence(timeout: 10), "未推入快照页面：\(name)")
            XCTAssertTrue(app.descendants(matching: .any)["extension-ui-ready"].waitForExistence(timeout: 10),
                          "快照未加载：\(name)\n\(app.debugDescription)")
            let screenshot = app.screenshot()
            let appearance = env["PERSONA_EXTENSIONS_APPEARANCE"] ?? "light"
            try screenshot.pngRepresentation.write(to: URL(fileURLWithPath: dir)
                .appending(path: "showcase-\(name)-\(UITestStyle.name)-\(appearance).png"))
            if name == "row" || name == "field" || name == "chart" || name == "stat_grid" || name == "table" {
                for _ in 0..<3 { app.swipeUp() }
                try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir)
                    .appending(path: "showcase-\(name)-bottom-\(UITestStyle.name)-\(appearance).png"))
            }
            app.navigationBars.buttons.element(boundBy: 0).tap()
            XCTAssertTrue(app.navigationBars.staticTexts["共享组件快照"].waitForExistence(timeout: 10), "没有回到快照目录：\(name)")
        }
    }
}
