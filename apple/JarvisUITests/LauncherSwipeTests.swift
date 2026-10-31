import XCTest

/// 设置 › 模型: swiping a ~/.zshrc launcher left hides it (it moves under 已隐藏), swiping a hidden one brings it back.
/// A Jarvis launcher swiped left is deleted after a confirmation.
/// Against scripts/mock_hub.py with MOCK_MANAGED_LAUNCHER=1 (jv_strong added in Jarvis; cc_glm shown, cc_old and cc_test hidden):
///   TEST_RUNNER_JARVIS_LAUNCHER_HUB=http://127.0.0.1:<port>
final class LauncherSwipeTests: XCTestCase {
    @MainActor func testSwipeHidesAndRestores() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_LAUNCHER_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_LAUNCHER_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openModelLaunchers", "YES", "-dev.openChat", ""]
        app.launch()

        let glm = app.cells.containing(.staticText, identifier: "cc_glm").firstMatch
        XCTAssertTrue(glm.waitForExistence(timeout: 15), "no cc_glm:\n\(app.debugDescription)")
        let folded = { (n: Int) in app.buttons["已隐藏 \(n) 个，已折叠"] }
        XCTAssertTrue(folded(2).exists, "已隐藏 should start folded with 2")

        glm.swipeLeft()
        let hide = app.buttons["隐藏"]
        XCTAssertTrue(hide.waitForExistence(timeout: 3), "no 隐藏 under the swipe:\n\(app.debugDescription)")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/launcher-swipe.png"))
        hide.tap()
        XCTAssertTrue(folded(3).waitForExistence(timeout: 5), "cc_glm did not move under 已隐藏")
        XCTAssertFalse(app.staticTexts["cc_glm"].exists, "cc_glm is still listed while 已隐藏 is folded")

        folded(3).tap()
        let hidden = app.cells.containing(.staticText, identifier: "cc_glm").firstMatch
        XCTAssertTrue(hidden.waitForExistence(timeout: 3), "cc_glm not under 已隐藏 once opened")
        hidden.swipeLeft()
        let restore = app.buttons["恢复"]
        XCTAssertTrue(restore.waitForExistence(timeout: 3), "no 恢复 under the swipe:\n\(app.debugDescription)")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/launcher-restore.png"))
        restore.tap()
        XCTAssertTrue(app.buttons["已隐藏 2 个，已展开"].waitForExistence(timeout: 5), "cc_glm did not come back")

        let jv = app.cells.containing(.staticText, identifier: "jv_strong").firstMatch
        XCTAssertTrue(jv.exists, "no jv_strong (launch the mock with MOCK_MANAGED_LAUNCHER=1)")
        jv.swipeLeft()
        let delete = app.buttons["删除"]
        XCTAssertTrue(delete.waitForExistence(timeout: 3), "no 删除 under the swipe:\n\(app.debugDescription)")
        delete.tap()
        XCTAssertTrue(app.staticTexts["删除启动器「jv_strong」？"].waitForExistence(timeout: 3), "no confirmation:\n\(app.debugDescription)")
        XCTAssertTrue(jv.exists, "deleted before the confirmation")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/launcher-delete.png"))
        // The dialog's own 删除 (a sheet on a phone); the swipe's one is gone once the dialog is up.
        let deletes = app.sheets.buttons.matching(identifier: "删除")
        (deletes.count > 0 ? deletes : app.buttons.matching(identifier: "删除")).firstMatch.tap()
        XCTAssertTrue(jv.waitForNonExistence(timeout: 5), "jv_strong is still listed")
    }
}
