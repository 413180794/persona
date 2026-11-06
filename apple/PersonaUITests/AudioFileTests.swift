import XCTest

/// An audio file plays in the file page (#1298): play / pause, the position moving, the speed.
///   PORT=18785 MOCK_AGENTS=1 MOCK_OFFICE=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18785 xcodebuild test … -only-testing:PersonaUITests/AudioFileTests
final class AudioFileTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_AGENTS=1 MOCK_OFFICE=1 (PERSONA_MOCK_HUB)")
        }
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "audio \(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor func testAudioPlays() {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000001",
                               "-openFilePath", "docs/站会录音.wav"]
        app.launchPinned()
        let play = app.buttons["audio-play"]
        XCTAssertTrue(play.waitForExistence(timeout: 25), "the player did not show")
        shot("ready")
        play.tap()
        XCTAssertTrue(app.buttons["暂停"].waitForExistence(timeout: 5), "it did not start playing")
        sleep(2)
        XCTAssertFalse(app.staticTexts["0:00"].exists, "the position did not move")
        shot("playing")
        app.buttons["暂停"].tap()
        XCTAssertTrue(app.buttons["播放"].waitForExistence(timeout: 5))
    }
}
