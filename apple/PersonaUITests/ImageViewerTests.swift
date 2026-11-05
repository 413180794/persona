import XCTest

/// The image viewer (docs/ios.md「图片查看器」): every button of its bars inside the screen (they ran off both sides on an
/// iPhone, 2026-11-05), a tap hides and shows the bars, a pull down closes it. Run it on the narrowest phone too (SE).
///   PORT=18831 MOCK_TURN_IMAGES=many python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18831 xcodebuild test … -only-testing:PersonaUITests/ImageViewerTests
final class ImageViewerTests: XCTestCase {
    private var hub = ""
    /// The blog conversation, whose last turn looked at five pictures (MOCK_TURN_IMAGES=many).
    private let blog = "studio/a1f3c2d4-0000-4000-8000-000000000003"

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TURN_IMAGES=many (JARVIS_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = false
    }

    @MainActor private func open() -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", blog, "-openViewedImage", "2"]
        app.launchPinned()
        XCTAssertTrue(app.buttons["viewer-close"].waitForExistence(timeout: 30), "the viewer did not open:\n\(app.debugDescription)")
        return app
    }

    @MainActor func testBarsFitTheScreen() throws {
        let app = open()
        let screen = app.windows.firstMatch.frame
        let controls: [XCUIElement] = [
            app.buttons["viewer-close"], app.descendants(matching: .any)["viewer-title"], app.buttons["viewer-more"],
            app.buttons["分享"], app.buttons["保存"], app.buttons["标注"], app.buttons["信息"], app.buttons["拷贝"],
        ]
        for c in controls {
            XCTAssertTrue(c.waitForExistence(timeout: 10), "missing \(c):\n\(app.debugDescription)")
            XCTAssertTrue(screen.contains(c.frame), "\(c.label) at \(c.frame) is not inside the screen \(screen)")
        }
        // Several pictures: the strip of thumbnails, and the title says which one.
        XCTAssertTrue(app.descendants(matching: .any)["viewer-thumbs"].exists)
        XCTAssertTrue(app.descendants(matching: .any)["viewer-title"].label.contains("2 / 5"))
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = "image-viewer"
        shot.lifetime = .keepAlways
        add(shot)
    }

    @MainActor func testTapHidesTheBarsAndPullDownCloses() throws {
        let app = open()
        let close = app.buttons["viewer-close"]
        let middle = app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        middle.tap()
        XCTAssertTrue(waitUntil { !close.isHittable }, "the bars stayed after a tap")
        middle.tap()
        XCTAssertTrue(waitUntil { close.isHittable }, "the bars did not come back")
        // A thumbnail moves to its picture.
        app.buttons["第 1 张"].tap()
        XCTAssertTrue(waitUntil { app.descendants(matching: .any)["viewer-title"].label.contains("1 / 5") })
        middle.press(forDuration: 0.05, thenDragTo: app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.9)))
        XCTAssertTrue(close.waitForNonExistence(timeout: 5), "a pull down did not close the viewer")
    }

    private func waitUntil(_ timeout: TimeInterval = 5, _ ok: @escaping () -> Bool) -> Bool {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if ok() { return true }
            RunLoop.current.run(until: Date().addingTimeInterval(0.2))
        }
        return ok()
    }
}
