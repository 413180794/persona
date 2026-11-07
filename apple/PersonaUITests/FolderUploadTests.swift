import XCTest

/// Files on a machine's page and an upload into its home (#4858, docs/protocol/protocol.md "Changing files"), against
/// scripts/mock/mock_hub.py started with MOCK_FILES=1 MOCK_FILE_OPS=1:
///   TEST_RUNNER_PERSONA_FILES_HUB=http://127.0.0.1:<port>
/// The upload is the DEBUG `-showFolderUpload` file (no picker to drive); its name is taken, so the alert comes first.
/// TEST_RUNNER_PERSONA_SHOTS=<dir> also saves a screenshot of each step there.
final class FolderUploadTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_FILES_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_FILES=1 MOCK_FILE_OPS=1 (PERSONA_FILES_HUB)")
        }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-openMachineDetail", "macbook",
                               "-showFolderUpload", "notes.txt"]
        app.launchPinned()
    }

    private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] {
            let look = ProcessInfo.processInfo.environment["PERSONA_SHOTS_APPEARANCE"].map { "-" + $0 } ?? ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + look + ".png"))
        }
    }

    func testATakenNameAsksAndKeepsBoth() {
        let files = app.buttons["machine.files"]
        XCTAssertTrue(files.waitForExistence(timeout: 40), "no 文件 row on the machine page")
        for _ in 0..<6 where !files.isHittable { app.swipeUp(velocity: .slow) }
        shot("files-row")
        files.tap()
        XCTAssertTrue(app.buttons["files.upload"].waitForExistence(timeout: 10), "no upload button in the browser")
        XCTAssertTrue(app.staticTexts["notes.txt"].waitForExistence(timeout: 10))
        shot("files-home")

        let alert = app.alerts["「notes.txt」已存在"]
        XCTAssertTrue(alert.waitForExistence(timeout: 20), "the taken name did not ask")
        XCTAssertTrue(alert.buttons["取消"].exists || alert.buttons["Cancel"].exists, "the question has no cancel")
        shot("files-conflict")
        alert.buttons["保留两者"].tap()

        XCTAssertTrue(app.staticTexts["notes 2.txt"].waitForExistence(timeout: 60), "the kept copy never showed up")
        XCTAssertTrue(app.staticTexts["notes.txt"].exists, "the old file went away")
        XCTAssertFalse(app.staticTexts["0 KB"].exists || app.staticTexts["Zero KB"].exists, "a size outside the one format")
        shot("files-kept")

        // Save to Files from the long-press menu: downloaded, then the system's place picker.
        app.staticTexts["notes.txt"].press(forDuration: 1.2)
        let save = app.buttons["存到「文件」"]
        XCTAssertTrue(save.waitForExistence(timeout: 5), "no 存到「文件」 in the menu")
        shot("files-menu")
        save.tap()
        XCTAssertTrue(save.waitForNonExistence(timeout: 5), "the menu stayed")
        // The place picker runs in another process: its buttons are not in this app's tree, but it covers the browser.
        let upload = app.buttons["files.upload"]
        let covered = expectation(for: NSPredicate(format: "hittable == false"), evaluatedWith: upload)
        wait(for: [covered], timeout: 30)
        sleep(2)
        shot("files-save")
    }
}
