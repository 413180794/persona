import XCTest

/// Real pairing completion and Settings persist the same engine contract against the isolated mock hub.
final class EngineChoiceTests: XCTestCase {
    private var hub = ""
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let value = ProcessInfo.processInfo.environment["JARVIS_ENGINE_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_ENGINE_HUB)")
        }
        hub = value
        _ = try Data(contentsOf: URL(string: hub + "/mock/reset")!)
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.lifetime = .keepAlways
            add(shot)
            add(XCTAttachment(string: app.debugDescription))
        }
    }

    private var pair: String {
        "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
    }

    @MainActor private func shot(_ name: String) throws {
        let screenshot = app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            let style = ProcessInfo.processInfo.environment["JARVIS_STYLE"] ?? "light"
            try screenshot.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("engine-\(name)-\(style).png"))
        }
    }

    private func current(_ machine: String) throws -> String? {
        let data = try Data(contentsOf: URL(string: hub + "/api/v1/machines")!)
        let root = try JSONSerialization.jsonObject(with: data) as? [String: Any]
        return (root?["machines"] as? [[String: Any]])?.first { $0["id"] as? String == machine }?["engine"] as? String
    }

    /// The short Duo cover can put the last Form row below the viewport; reach it before tapping.
    @MainActor private func skipEngine() {
        let skip = app.buttons["engine.skip"]
        for _ in 0..<4 {
            if skip.waitForExistence(timeout: 1) { break }
            app.collectionViews.firstMatch.swipeUp(velocity: .slow)
        }
        XCTAssertTrue(skip.waitForExistence(timeout: 5), "no reachable way to skip engine selection")
        // A Form cell can exist just below the viewport; native tap finishes scrolling it into view.
        skip.tap()
    }

    /// UIKit renders the four tabs in a side rail on Duo's cover, without a TabBar accessibility parent.
    @MainActor private func pairedRoot() {
        for title in ["Jarvis", "Agents", "收件箱", "设置"] {
            XCTAssertTrue(app.buttons[title].firstMatch.waitForExistence(timeout: 10), "no paired root control: \(title)")
        }
        XCTAssertFalse(app.buttons["engine.save"].exists, "the engine step did not finish")
        XCTAssertFalse(app.buttons["engine.skip"].exists, "the engine step is still open")
    }

    @MainActor func testPairingCanChooseAndSkip() throws {
        app = XCUIApplication()
        app.launchArguments = ["-openWelcome", "YES", "-pairURL", ""]
        app.launchJarvis()
        XCTAssertTrue(app.buttons["welcome.pair"].waitForExistence(timeout: 10))
        app.open(URL(string: pair)!)
        XCTAssertTrue(app.buttons["engine.save"].waitForExistence(timeout: 15))
        XCTAssertTrue(app.staticTexts["这台电脑用哪个引擎"].exists)
        try shot("pairing")
        app.buttons["engine.option.herdr-core"].tap()
        app.buttons["engine.save"].tap()
        pairedRoot()
        XCTAssertEqual(try current("server"), "herdr-core")
        app.open(URL(string: pair)!)
        XCTAssertTrue(app.buttons["engine.save"].waitForExistence(timeout: 10))
        skipEngine()
        pairedRoot()
        XCTAssertEqual(try current("server"), "herdr-core", "skipping must preserve the configured engine")
    }

    @MainActor func testSettingsEngineRowAndPicker() throws {
        app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "server"]
        app.launchJarvis()
        let row = app.buttons["machine.engine"]
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        if !row.isHittable { app.swipeUp() }
        try shot("settings-row")
        row.tap()
        XCTAssertTrue(app.buttons["engine.save"].waitForExistence(timeout: 10))
        app.buttons["engine.option.herdr-core"].tap()
        try shot("settings-picker")
        app.buttons["engine.save"].tap()
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        XCTAssertTrue(row.label.contains("Herdr App"), row.label)
        XCTAssertEqual(try current("server"), "herdr-core")
    }
    @MainActor func testSettingsTracksPushAndSavesOffline() async throws {
        app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "nas"]
        app.launchJarvis()
        let row = app.buttons["machine.engine"]
        XCTAssertTrue(row.waitForExistence(timeout: 15))
        if !row.isHittable { app.swipeUp() }
        var request = URLRequest(url: URL(string: hub + "/api/v1/machines/nas/engine")!)
        request.httpMethod = "PUT"
        request.httpBody = Data("{\"engine\":\"herdr-core\"}".utf8)
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        _ = try await URLSession.shared.data(for: request)
        let pushed = app.buttons.matching(NSPredicate(format: "identifier == %@ AND label CONTAINS %@", "machine.engine", "Herdr App")).firstMatch
        XCTAssertTrue(pushed.waitForExistence(timeout: 10), "the row follows the machines push")
        row.tap()
        XCTAssertTrue(app.staticTexts["离线，暂无检测信息"].firstMatch.waitForExistence(timeout: 10))
        app.buttons["engine.option.herdr"].tap()
        app.buttons["engine.save"].tap()
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        XCTAssertEqual(try current("nas"), "herdr")
    }

    @MainActor func testManualPairingShowsEngineStep() throws {
        app = XCUIApplication()
        app.launchArguments = ["-openWelcome", "YES", "-pairURL", ""]
        app.launchJarvis()
        XCTAssertTrue(app.buttons["welcome.pair"].waitForExistence(timeout: 10))
        app.buttons["welcome.pair"].tap()
        let address = app.textFields["服务器地址"]
        XCTAssertTrue(address.waitForExistence(timeout: 10))
        address.tap()
        address.typeText(hub)
        app.secureTextFields["Token"].tap()
        app.secureTextFields["Token"].typeText("mock")
        app.navigationBars.buttons["连接"].tap()
        XCTAssertTrue(app.buttons["engine.save"].waitForExistence(timeout: 15))
        skipEngine()
        pairedRoot()
        XCTAssertEqual(try current("server"), "herdr")
    }

    @MainActor func testPairingFailureCanContinue() throws {
        guard let errorHub = ProcessInfo.processInfo.environment["JARVIS_ENGINE_ERROR_HUB"] else {
            throw XCTSkip("needs MOCK_ENGINE_ERROR=1 hub")
        }
        hub = errorHub
        app = XCUIApplication()
        app.launchArguments = ["-openWelcome", "YES", "-pairURL", ""]
        app.launchJarvis()
        app.open(URL(string: pair)!)
        XCTAssertTrue(app.buttons["engine.save"].waitForExistence(timeout: 15))
        app.buttons["engine.option.herdr-core"].tap()
        app.buttons["engine.save"].tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "保存失败")).firstMatch.waitForExistence(timeout: 10))
        try shot("pairing-error")
        skipEngine()
        pairedRoot()
        XCTAssertEqual(try current("server"), "herdr")
    }

}
