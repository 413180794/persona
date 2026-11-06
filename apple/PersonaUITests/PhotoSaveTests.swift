import XCTest

/// Saving to the photo library for real (scripts/mock/mock_hub.py, JARVIS_MOCK_HUB): the image viewer's 保存
/// and the share card's 保存, each after allowing the system's permission alert. Photos runs the change block on its
/// own queue; a block the compiler took for main-actor code crashed the app there (2026-10-31), so the test only
/// passes if the app is still alive and says 已保存到相册.
/// The conversation with the screenshot is put in by the test (GET /mock/photo?on=1) rather than by starting the mock
/// with MOCK_PHOTO=1: a mock started the usual way had no such conversation, the chat opened empty and both failed (#2334).
final class PhotoSaveTests: XCTestCase {
    private var hub: String!
    private var photoWas = true
    private let session = "macbook/a1f3c2d4-0000-4000-8000-00000000000c"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
        let state = try mockPhoto(true)
        XCTAssertEqual(state["photo"] as? Bool, true, "the mock did not put in the conversation with the screenshot: \(state)")
        photoWas = state["was"] as? Bool ?? true
    }

    override func tearDownWithError() throws {
        if !photoWas { _ = try? mockPhoto(false) } // the tests after this one see the mock as it was
    }

    /// GET /mock/photo?on=1|0 puts the conversation with the screenshot in or takes it out; answers {"photo", "was"}.
    private func mockPhoto(_ on: Bool) throws -> [String: Any] {
        let data = try Data(contentsOf: URL(string: hub + "/mock/photo?on=" + (on ? "1" : "0"))!)
        return try XCTUnwrap(try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                             "no /mock/photo in this mock (an older scripts/mock/mock_hub.py?)")
    }

    @MainActor private func launch(_ extra: [String]) -> XCUIApplication {
        let app = XCUIApplication()
        app.resetAuthorizationStatus(for: .photos) // the alert comes up again, like on a fresh install
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", session] + extra
        app.launchPinned()
        return app
    }

    /// Taps 允许 on the photo library alert (worded in the simulator's language) and checks the app saved.
    @MainActor private func allowAndExpectSaved(_ app: XCUIApplication, _ name: String) {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let allow = springboard.alerts.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@", "允许", "Allow")).firstMatch
        XCTAssertTrue(allow.waitForExistence(timeout: 10), "no photo library alert:\n\(springboard.debugDescription)")
        allow.tap()
        let saved = app.staticTexts["已保存到相册"]
        XCTAssertTrue(saved.waitForExistence(timeout: 10), "no 已保存到相册 (state \(app.state.rawValue)):\n\(app.debugDescription)")
        XCTAssertEqual(app.state, .runningForeground, "the app is gone after saving")
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }

    @MainActor func testSaveFromTheImageViewer() throws {
        let app = launch(["-openFirstImage", "YES"])
        let save = app.buttons["保存"]
        XCTAssertTrue(save.waitForExistence(timeout: 20), "the image viewer did not open:\n\(app.debugDescription)")
        XCTAssertTrue(NSPredicate(format: "isEnabled == true").evaluate(with: save) || {
            _ = XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "isEnabled == true"), object: save)], timeout: 10)
            return save.isEnabled
        }(), "保存 stays disabled: the image never decoded")
        save.tap()
        allowAndExpectSaved(app, "photo-save-viewer")
    }

    @MainActor func testSaveTheShareCard() throws {
        let app = launch(["-showShareCard", "YES"])
        let save = app.buttons["保存"]
        XCTAssertTrue(save.waitForExistence(timeout: 20), "the share card did not open:\n\(app.debugDescription)")
        _ = XCTWaiter.wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "isEnabled == true"), object: save)], timeout: 10)
        save.tap()
        allowAndExpectSaved(app, "photo-save-card")
    }
}
