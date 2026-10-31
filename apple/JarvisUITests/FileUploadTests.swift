import XCTest

/// Files sent to an agent (docs/protocol.md「发送文件」), against scripts/mock_hub.py
/// (TEST_RUNNER_JARVIS_UPLOAD_HUB=http://127.0.0.1:<port>; MOCK_UPLOAD_KBPS slows it down enough to see the bar).
final class FileUploadTests: XCTestCase {
    private let widget = "a1f3c2d4-0000-4000-8000-000000000001"

    private func launch(_ args: [String]) throws -> XCUIApplication {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_UPLOAD_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_UPLOAD_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/" + widget] + args
        app.launch()
        return app
    }

    /// The + menu offers 文件 next to 照片 and 拍照.
    @MainActor func testMenuHasFiles() throws {
        let app = try launch([])
        let plus = app.buttons["Add"].exists ? app.buttons["Add"] : app.buttons["添加"]
        XCTAssertTrue(plus.waitForExistence(timeout: 15), "no + button:\n\(app.debugDescription)")
        plus.tap()
        XCTAssertTrue(app.buttons["文件"].waitForExistence(timeout: 5), "no 文件 item:\n\(app.debugDescription)")
    }

    /// An upload shows its progress with a cancel button, then its path lands in the input box, unsent.
    @MainActor func testUploadInsertsThePath() throws {
        let app = try launch(["-openUploadDemo", "季度报告.pdf"])
        let cancel = app.buttons["取消上传 季度报告.pdf"]
        XCTAssertTrue(cancel.waitForExistence(timeout: 15), "no progress bar:\n\(app.debugDescription)")
        // Typing while it uploads: the path goes after what was typed (at the cursor), nothing is sent.
        let field = app.textFields.firstMatch.exists ? app.textFields.firstMatch : app.textViews.firstMatch
        field.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.9)).tap() // the end of a draft left by an earlier run
        let left = (field.value as? String ?? "").count
        if left > 0 { field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: left + 2)) }
        field.typeText("look at")
        XCTAssertEqual(field.value as? String, "look at", "typing alone")
        let inserted = NSPredicate { _, _ in ((field.value as? String) ?? "").contains("/tmp/季度报告-") }
        expectation(for: inserted, evaluatedWith: nil)
        waitForExpectations(timeout: 90)
        XCTAssertFalse(cancel.exists, "the bar goes once the file is there")
        let value = field.value as? String ?? ""
        XCTAssertTrue(value.hasPrefix("look at /tmp/季度报告-") && value.hasSuffix(".pdf "), value)
    }
}
