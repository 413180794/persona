import XCTest

/// Settings › Machines: a machine whose personad is not the hub's build has Redeploy on its card. Tapping it (inside the card's
/// link) deploys instead of opening the machine, shows the progress, and the warning goes once the hub says it is done.
/// Against scripts/mock/mock_hub.py with MOCK_NODE_MISMATCH=1 (Mac Studio mismatched; its deploy takes 3 s):
///   TEST_RUNNER_PERSONA_DEPLOY_HUB=http://127.0.0.1:<port>
final class DeployButtonTests: XCTestCase {
    @MainActor func testRedeployFromTheMachineList() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_DEPLOY_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_NODE_MISMATCH=1 (PERSONA_DEPLOY_HUB)")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openMachines", "YES", "-dev.openChat", ""]
        app.launchPinned()
        let deploy = app.buttons["重新部署"]
        XCTAssertTrue(deploy.waitForExistence(timeout: 15), "no 重新部署:\n\(app.debugDescription)")
        if !deploy.isHittable { app.swipeUp() }
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/deploy-before.png"))
        deploy.tap()
        XCTAssertTrue(app.buttons["正在部署"].waitForExistence(timeout: 5), "no progress after the tap:\n\(app.debugDescription)")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/deploy-during.png"))
        XCTAssertTrue(app.navigationBars["机器"].exists || app.staticTexts["机器"].exists, "the tap opened the machine instead")
        let warning = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "不是同一次部署")).firstMatch
        XCTAssertTrue(app.buttons["正在部署"].waitForNonExistence(timeout: 15), "still deploying")
        XCTAssertFalse(warning.exists, "the warning stayed after the deploy")
        XCTAssertFalse(app.buttons["重新部署"].exists, "重新部署 stayed after the deploy")
    }
}
