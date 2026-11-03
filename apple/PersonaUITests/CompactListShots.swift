import XCTest

/// The Agents list's 细列表 on 体验模式's data, in every density it has to hold together: many spaces (全部), a machine
/// with many finished-unread agents and idle spaces (Mac Studio), the 未读 and 需要你 filters, a machine where nothing runs
/// (Mac mini), and the cards for comparison; at each text size (小 / 标准 / 大). No hub needed (`-openWelcome` drops a
/// pairing left by other tests; the launch argument keeps 体验模式 on).
///   TEST_RUNNER_JARVIS_SHOTS=<dir> TEST_RUNNER_JARVIS_STYLE=light|dark
/// writes `compact-<state>-<size>-<style>.png` there (docs/testing.md「Agents 列表」).
final class CompactListShots: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    @MainActor private func launch(machine: String, compact: Bool, size: String, grouping: String = "space") {
        app = XCUIApplication()
        app.launchArguments = ["-openWelcome", "YES", "-demo.on", "YES", "-tab", "dev", "-dev.openChat", "", "-openAgentsFresh", "YES",
                               "-dev.machineFilter", machine, "-dev.compact", compact ? "YES" : "NO", "-ui.textSize", size,
                               "-dev.grouping", grouping]
        app.launchPinned()
        XCTAssertTrue(app.buttons["agents.filter.all"].waitForExistence(timeout: 15), "the Agents list")
        sleep(2)
    }

    @MainActor private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            let style = ProcessInfo.processInfo.environment["JARVIS_STYLE"].map { "-" + $0 } ?? ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + style + ".png"))
        }
    }

    @MainActor func testShots() throws {
        let sizes = ProcessInfo.processInfo.environment["JARVIS_TEXT_SIZES"]?.split(separator: ",").map(String.init)
            ?? ["small", "standard", "large"]
        for size in sizes {
            launch(machine: "", compact: true, size: size)
            shot("compact-all-\(size)")
            app.swipeUp(velocity: .slow)
            sleep(1)
            shot("compact-all-scrolled-\(size)")
            launch(machine: "studio", compact: true, size: size)
            shot("compact-studio-\(size)")
            app.buttons["agents.filter.unread"].tap()
            sleep(1)
            shot("compact-unread-\(size)")
            launch(machine: "mini", compact: true, size: size)
            shot("compact-idle-\(size)")
            launch(machine: "", compact: true, size: size)
            app.buttons["agents.filter.needsYou"].tap()
            sleep(1)
            shot("compact-needs-\(size)")
            launch(machine: "", compact: true, size: size, grouping: "state")
            shot("compact-bystate-\(size)")
            launch(machine: "studio", compact: false, size: size)
            shot("cards-studio-\(size)")
        }
    }
}
