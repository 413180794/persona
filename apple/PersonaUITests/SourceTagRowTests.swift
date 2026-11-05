import XCTest

/// The BeautifulUI style's Agents rows (#3004): the 终端 / 飞书群 tag sits on the title's line, before the title, and the
/// state badge keeps its column to itself. The tag used to share the badge's 24 pt column, which spilled both ways: the
/// badge half out of the capsule and the tag over the title's first characters.
///   MOCK_BOTMUX=1 PORT=<port> python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<port> xcodebuild test … -only-testing:PersonaUITests/SourceTagRowTests
/// `TEST_RUNNER_JARVIS_SHOTS=<dir>` keeps the list (`…_SHOTS_APPEARANCE` names the simulator's appearance).
final class SourceTagRowTests: XCTestCase {
    /// The capsule's own padding and the badge's column (`TaskRow.badgeColumn` less its gap): a tag starts after them.
    private static let badgeColumnEnd: CGFloat = 10 + 24

    @MainActor func testTheTagSitsBeforeTheTitleNotInTheBadgeColumn() throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["JARVIS_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (JARVIS_MOCK_HUB)") }
        continueAfterFailure = false
        _ = try Data(contentsOf: URL(string: hub + "/mock/reset")!)
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-tab", "dev", "-openAgentsFresh", "YES", "-ui.style", "beautiful"]
        app.launchPersona()

        // The 需要你处理 cards fill the first screen: the list's own 终端 / 飞书群 filters bring those rows up.
        for (filter, label) in [("terminal", "终端"), ("feishu", "飞书群")] {
            if filter == "feishu" && env["JARVIS_BOTMUX"] == nil { continue }
            let chip = app.buttons["agents.filter." + filter]
            XCTAssertTrue(chip.waitForExistence(timeout: 20), "no \(label) filter:\n\(app.debugDescription)")
            chip.tap()
            let tags = app.descendants(matching: .any).matching(identifier: "agents.row.source")
            XCTAssertTrue(tags.firstMatch.waitForExistence(timeout: 10), "no \(label) tag in the list:\n\(app.debugDescription)")
            sleep(1)
            checkTags(app, label)
            shot(app, filter)
            chip.tap() // off again
        }
    }

    /// Every tag on screen starts after its row's badge column, on its row.
    @MainActor private func checkTags(_ app: XCUIApplication, _ label: String) {
        let rows = app.descendants(matching: .any).matching(identifier: "agents.row").allElementsBoundByIndex.map(\.frame)
        let screen = app.windows.firstMatch.frame
        var checked = 0
        for tag in app.descendants(matching: .any).matching(identifier: "agents.row.source").allElementsBoundByIndex {
            let f = tag.frame
            guard !f.isEmpty, screen.contains(f), let row = rows.first(where: { $0.minY <= f.midY && f.midY <= $0.maxY }) else { continue }
            XCTAssertGreaterThanOrEqual(f.minX - row.minX, Self.badgeColumnEnd,
                                        "the \(label) tag at \(f) is in the badge's column of the row at \(row)")
            checked += 1
        }
        XCTAssertGreaterThan(checked, 0, "no \(label) tag on a row on screen to check:\n\(app.debugDescription)")
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        let env = ProcessInfo.processInfo.environment
        guard let dir = env["JARVIS_SHOTS"] else { return }
        let look = env["JARVIS_SHOTS_APPEARANCE"] ?? "light"
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("source-tag-\(name)-\(look).png"))
    }
}
