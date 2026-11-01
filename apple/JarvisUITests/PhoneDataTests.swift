import XCTest

/// 设置 › 连接器 › 这台 iPhone: the switch is the whole state, no 已授权 / 点此授权 labels; a source never asked is off.
/// (The system's permission alert is not driven here: on a busy machine it took minutes and came back from earlier runs.) Against scripts/mock_hub.py (JARVIS_MOCK_HUB), on a fresh simulator.
final class PhoneDataTests: XCTestCase {
    @MainActor func testSwitchIsTheGrant() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", "", "-ui.oldLayout", "NO"]
        app.launchPinned()
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "这台 iPhone")).firstMatch
        for _ in 0..<6 where !(row.exists && row.isHittable) { app.swipeUp() }
        row.tap()
        let contacts = app.switches["通讯录"]
        XCTAssertTrue(contacts.waitForExistence(timeout: 10), "no 通讯录 switch:\n\(app.debugDescription)")
        XCTAssertEqual(contacts.value as? String, "0", "never asked, yet on")
        XCTAssertFalse(app.staticTexts["点此授权"].exists || app.staticTexts["已授权"].exists, "the old labels are still there")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/pd-before.png"))

    }
}
