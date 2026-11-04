import XCTest

/// A plugin's list of machines (schema format machine, docs/plugins.md): a switch per Mac, by name and how it is, no
/// machine ids typed in; a switch takes effect at once and is the machine page's 允许助手操控这台电脑 too. Against
/// scripts/mock/mock_hub.py with MOCK_PLUGINS=1 MOCK_COMPUTER=ready (`TEST_RUNNER_JARVIS_PLUGINS_HUB`), which has the
/// plugin cua on for MacBook; `TEST_RUNNER_JARVIS_SHOTS` keeps the screenshots, named by
/// `TEST_RUNNER_JARVIS_PLUGINS_APPEARANCE`.
final class PluginMachineSwitchesTests: XCTestCase {
    @MainActor func testMacsAreSwitches() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["JARVIS_PLUGINS_HUB"], env["JARVIS_PLUGINS_COMPUTER"] == "1" else {
            throw XCTSkip("needs mock_hub.py with MOCK_PLUGINS=1 MOCK_COMPUTER=ready (JARVIS_PLUGINS_HUB, JARVIS_PLUGINS_COMPUTER=1)")
        }
        continueAfterFailure = false
        let look = env["JARVIS_PLUGINS_APPEARANCE"] ?? "light"
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openPlugin", "cua"]
        app.launchPinned()

        // By name and state (the field's identifier is on the whole row, as for every field).
        func row(_ name: String) -> XCUIElement { app.switches.matching(NSPredicate(format: "label BEGINSWITH %@", name + "、")).firstMatch }
        let macbook = row("MacBook"), studio = row("Mac Studio")
        XCTAssertTrue(macbook.waitForExistence(timeout: 30), "no switch per Mac:\n\(app.debugDescription)")
        XCTAssertTrue(studio.exists)
        XCTAssertFalse(row("服务器").exists, "a Linux machine is not listed")
        XCTAssertFalse(app.textFields["添加一项"].exists, "no machine ids typed in")
        XCTAssertEqual(macbook.value as? String, "1")
        XCTAssertEqual(studio.value as? String, "0")
        XCTAssertTrue(macbook.label.contains("MacBook") && macbook.label.contains("在线 · Cua 可以用"), macbook.label)
        XCTAssertTrue(studio.label.contains("在线 · Cua 没装"), "said for a Mac it is not on for too: \(studio.label)")
        if let dir = env["JARVIS_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "plugins-machines-\(look).png"))
        }

        // In effect at once, no 保存: the hub turns it on for that Mac (the machine page's switch).
        studio.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitFor(studio, value: "1"), "turned on")
        XCTAssertTrue(waitUntil { (try? self.enabledMacs(hub)) == ["macbook", "studio"] }, "the hub has it: \(String(describing: try? enabledMacs(hub)))")
        XCTAssertFalse(app.buttons["plugin-save"].isEnabled, "nothing left to 保存")
        studio.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(waitUntil { (try? self.enabledMacs(hub)) == ["macbook"] }, "and off again")
    }

    /// A list with x-confirm (the plugin cua's 插件开着时不自动锁屏, #1507 b2): turning a Mac on asks first, 取消 leaves
    /// it off, 打开 turns it on (the hub has it); turning it off does not ask.
    @MainActor func testAListWithAConfirmAsksFirst() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["JARVIS_PLUGINS_HUB"], env["JARVIS_PLUGINS_COMPUTER"] == "1" else {
            throw XCTSkip("needs mock_hub.py with MOCK_PLUGINS=1 MOCK_COMPUTER=ready (JARVIS_PLUGINS_HUB, JARVIS_PLUGINS_COMPUTER=1)")
        }
        continueAfterFailure = false
        let look = env["JARVIS_PLUGINS_APPEARANCE"] ?? "light"
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-openPlugin", "cua"]
        app.launchPinned()

        let noLock = app.switches.matching(NSPredicate(format: "identifier == %@ AND label BEGINSWITH %@", "plugin-field-no_auto_lock", "MacBook、")).firstMatch
        XCTAssertTrue(noLock.waitForExistence(timeout: 30), "no 插件开着时不自动锁屏 switch for MacBook:\n\(app.debugDescription)")
        for _ in 0..<6 where !noLock.isHittable { app.swipeUp() }
        usleep(800_000) // the page coasts on after a swipe, and a tap then only stops it
        XCTAssertEqual(noLock.value as? String, "0", "off by default")
        let flip = { noLock.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap() }
        let turnOn = app.buttons["打开"].firstMatch
        flip()
        XCTAssertTrue(turnOn.waitForExistence(timeout: 5), "asked first:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "坐在它前面的人能直接用它")).firstMatch.exists,
                      "the plugin's x-confirm:\n\(app.debugDescription)")
        if let dir = env["JARVIS_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "plugins-no-lock-ask-\(look).png"))
        }
        cancelDialog(app)
        XCTAssertTrue(waitUntil { !turnOn.exists })
        XCTAssertEqual(noLock.value as? String, "0", "取消: still off")
        XCTAssertEqual(try noLockMacs(hub), [])
        flip()
        XCTAssertTrue(turnOn.waitForExistence(timeout: 5))
        turnOn.tap()
        XCTAssertTrue(waitFor(noLock, value: "1"), "turned on")
        XCTAssertTrue(waitUntil { (try? self.noLockMacs(hub)) == ["macbook"] }, "the hub has it: \(String(describing: try? noLockMacs(hub)))")
        flip()
        XCTAssertTrue(waitUntil { (try? self.noLockMacs(hub)) == [] }, "and off again")
        XCTAssertFalse(turnOn.exists, "turning it off does not ask")
    }

    /// 取消 the dialog: its button where it has one, else a tap outside it (iOS 26 shows it as a popover by the switch,
    /// with no 取消).
    @MainActor private func cancelDialog(_ app: XCUIApplication) {
        let cancel = app.buttons["取消"].firstMatch
        let outside = app.otherElements["PopoverDismissRegion"].firstMatch
        if cancel.exists { cancel.tap() } else if outside.exists { outside.tap() } else {
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.04, dy: 0.5)).tap()
        }
    }

    /// The Macs the hub says 插件开着时不自动锁屏 is on for (machine.computer.no_auto_lock), by id.
    private func noLockMacs(_ hub: String) throws -> [String] {
        try machines(hub).filter { $0.computer?.no_auto_lock == true }.map(\.id).sorted()
    }

    private struct Machines: Decodable {
        struct M: Decodable {
            struct C: Decodable { var enabled: Bool; var no_auto_lock: Bool? }
            var id: String
            var computer: C?
        }
        var machines: [M]
    }

    private func machines(_ hub: String) throws -> [Machines.M] {
        var req = URLRequest(url: URL(string: hub + "/api/v1/machines")!)
        req.setValue("Bearer mock", forHTTPHeaderField: "Authorization")
        var out: Data?
        let done = DispatchSemaphore(value: 0)
        URLSession.shared.dataTask(with: req) { data, _, _ in out = data; done.signal() }.resume()
        done.wait()
        return try JSONDecoder().decode(Machines.self, from: out ?? Data()).machines
    }

    /// The Macs the hub says the plugin is on for (machine.computer.enabled), by id.
    private func enabledMacs(_ hub: String) throws -> [String] {
        try machines(hub).filter { $0.computer?.enabled == true }.map(\.id).sorted()
    }

    private func waitFor(_ e: XCUIElement, value: String) -> Bool {
        waitUntil { e.value as? String == value }
    }

    private func waitUntil(_ ok: () -> Bool) -> Bool {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline { if ok() { return true }; usleep(200_000) }
        return false
    }
}
