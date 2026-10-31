import XCTest

/// Screenshots of the Fleet Live Activity with made-up agents (`-showFleetSample`): Dynamic Island compact and expanded,
/// lock screen. Not a pass / fail test: look at the attachments (xcresulttool export attachments). Needs nothing else.
final class LiveActivityShots: XCTestCase {
    @MainActor func testShots() throws {
        let env = ProcessInfo.processInfo.environment
        guard let phase = env["JARVIS_FLEET_SAMPLE"] else { throw XCTSkip("set JARVIS_FLEET_SAMPLE=needsYou|working|done|current") }
        if phase != "current" { // "current": photograph whatever the activity shows now (after a real push)
            let app = XCUIApplication()
            app.launchArguments = ["-showFleetSample", phase, "-fleetDetails", env["JARVIS_FLEET_DETAILS"] ?? "NO",
                                   "-fleetIsland", env["JARVIS_FLEET_ISLAND"] ?? "a", "-tab", "settings"]
            app.launch()
            sleep(4)
        }
        XCUIDevice.shared.press(.home)
        sleep(2)
        shot("\(phase)-compact")
        usleep(400_000)
        shot("\(phase)-compact2") // compared with the first: does the working gear turn?

        // Long-press the island to expand it.
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        springboard.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 201, dy: 37)).press(forDuration: 1.5)
        sleep(2)
        shot("\(phase)-expanded")
        // The long press does not always take: a second try, shown separately.
        XCUIDevice.shared.press(.home)
        sleep(1)
        springboard.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 201, dy: 37)).press(forDuration: 2)
        sleep(2)
        shot("\(phase)-expanded2")
        XCUIDevice.shared.press(.home)
        sleep(1)

        // Lock, then wake to the lock screen.
        XCUIDevice.shared.perform(NSSelectorFromString("pressLockButton"))
        sleep(2)
        XCUIDevice.shared.perform(NSSelectorFromString("pressLockButton"))
        sleep(3)
        // First time only: the system asks whether Jarvis may show Live Activities (on the lock screen). Its 允许 is
        // also the activity's own button: with the activity showing (拒绝 is there), do not tap it.
        let allow = springboard.buttons.matching(NSPredicate(format: "label IN %@", ["Allow", "允许"])).firstMatch
        let ours = springboard.buttons.matching(NSPredicate(format: "label CONTAINS %@", "拒绝")).firstMatch
        if !ours.exists, allow.exists {
            allow.tap()
            sleep(2)
        }
        shot("\(phase)-lock")
        // Unlock again (swipe up; no passcode on the simulator): a locked simulator hangs the next test run.
        let bottom = springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.99))
        bottom.press(forDuration: 0.1, thenDragTo: springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3)))
        sleep(1)
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }
}
