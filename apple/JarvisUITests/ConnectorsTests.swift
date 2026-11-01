import XCTest

/// 设置 › 连接器: this device's data, the mail accounts (moved here from the inbox's ⋯ menu) and 飞书 open from the
/// settings list. Against scripts/mock_hub.py (JARVIS_MOCK_HUB).
final class ConnectorsTests: XCTestCase {
    @MainActor func testEachConnectorOpens() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", ""]
        app.launchPinned()
        func row(_ label: String) -> XCUIElement {
            let r = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", label)).firstMatch
            // From the top, down to it: after a back the list may be anywhere.
            for _ in 0..<3 { app.swipeDown() }
            for _ in 0..<6 where !(r.exists && r.isHittable) { app.swipeUp() }
            return r
        }
        func shot(_ name: String) {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/conn-\(name).png"))
        }
        func back() {
            let b = app.navigationBars.buttons.element(boundBy: 0)
            XCTAssertTrue(b.waitForExistence(timeout: 5))
            b.tap()
        }
        XCTAssertTrue(app.staticTexts["连接器"].waitForExistence(timeout: 15) || row("飞书").exists, "no 连接器 section")
        shot("list")

        row("飞书").tap()
        XCTAssertTrue(app.staticTexts["已连接"].waitForExistence(timeout: 10), "飞书 page shows no connection:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["/agent"].exists)
        shot("feishu")
        back()

        row("邮箱").tap()
        XCTAssertTrue(app.buttons["添加邮箱"].waitForExistence(timeout: 10), "mail page has no 添加邮箱:\n\(app.debugDescription)")
        XCTAssertFalse(app.buttons["完成"].exists, "pushed, the mail page should not have the sheet's 完成")
        shot("mail")
        back()

        row("这台 iPhone").tap()
        XCTAssertTrue(app.navigationBars["这台 iPhone"].waitForExistence(timeout: 10) || app.staticTexts["这台 iPhone"].exists, "no device page")
        XCTAssertTrue(app.switches.firstMatch.exists, "no data source toggles")
        shot("device")
    }
}
