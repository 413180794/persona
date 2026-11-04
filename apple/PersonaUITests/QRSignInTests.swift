import XCTest

/// Signing in to a connector by scanning a QR code with another app (#1351), against scripts/mock/mock_hub.py started
/// with MOCK_WECHAT=signed_out (JARVIS_MOCK_HUB_QR): 设置 › 微信 › 登录 shows the code the hub drew and 保存二维码,
/// then signs in on its own once the mock says it was confirmed (its sixth ask) (the page asks again every interval).
final class QRSignInTests: XCTestCase {
    override func setUpWithError() throws { continueAfterFailure = false }

    private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "\(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor func testScanToSignIn() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB_QR"] else { throw XCTSkip("needs a mock with MOCK_WECHAT=signed_out") }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-dev.openChat", ""]
        app.launchPinned()
        let any = app.descendants(matching: .any)

        // From 设置 (a page opened at launch can beat the pairing): the hub's connectors list 微信.
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "微信")).firstMatch
        for _ in 0..<8 where !row.isHittable { app.swipeUp() }
        XCTAssertTrue(row.isHittable, "no 微信 row in 设置:\n\(app.debugDescription)")
        row.tap()

        let signIn = any["connector.signIn"]
        XCTAssertTrue(signIn.waitForExistence(timeout: 15), "no 登录 微信:\n\(app.debugDescription)")
        signIn.tap()

        let code = any["connector.qrCode"]
        XCTAssertTrue(code.waitForExistence(timeout: 10), "no QR code:\n\(app.debugDescription)")
        sleep(1)
        shot(app, "wechat qr")
        // 保存二维码 is under the code, below the fold (the list makes rows out of sight only when shown).
        let share = any["connector.shareQR"]
        for _ in 0..<3 where !share.exists { app.swipeUp() }
        XCTAssertTrue(share.waitForExistence(timeout: 5), "no 保存二维码:\n\(app.debugDescription)")
        XCTAssertFalse(any["connector.next"].exists, "a 下一步 under a QR code")
        XCTAssertFalse(any["connector.openVerification"].exists, "the code's URL offered as a link")
        shot(app, "wechat qr save")

        // The page asks again on its own: signed in after the mock's sixth ask.
        let gone = NSPredicate(format: "exists == false")
        XCTAssertEqual(XCTWaiter.wait(for: [expectation(for: gone, evaluatedWith: code)], timeout: 15), .completed,
                       "not signed in on its own:\n\(app.debugDescription)")
        for _ in 0..<3 { app.swipeDown() }
        let account = any.matching(NSPredicate(format: "label CONTAINS %@", "示例用户")).firstMatch
        XCTAssertTrue(account.waitForExistence(timeout: 5), "the account is not shown:\n\(app.debugDescription)")
        sleep(1)
        shot(app, "wechat signed in")
    }
}
