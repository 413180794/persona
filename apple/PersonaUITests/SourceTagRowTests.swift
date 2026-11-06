import XCTest

/// The Agents rows' Terminal / Lark Group tag sits under the row's time, at its end (#3043), in the default and the BeautifulUI
/// style: not before the title, which it pushed onto two lines, nor in the badge's 24 pt column (#3004), which spilled
/// both ways (the badge half out of the capsule, the tag over the title's first characters).
///   MOCK_BOTMUX=1 PORT=<port> python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:<port> xcodebuild test … -only-testing:PersonaUITests/SourceTagRowTests
/// `TEST_RUNNER_PERSONA_SHOTS=<dir>` keeps the list (`…_SHOTS_APPEARANCE` names the simulator's appearance).
final class SourceTagRowTests: XCTestCase {
    /// The capsule's own padding and the badge's column (`TaskRow.badgeColumn` less its gap): a tag starts after them.
    private static let badgeColumnEnd: CGFloat = 10 + 24

    @MainActor func testTheTagSitsUnderTheTimeAtTheRowsEnd() throws {
        for style in ["glass", "beautiful"] { try check(style: style) }
    }

    @MainActor private func check(style: String) throws {
        let env = ProcessInfo.processInfo.environment
        guard let hub = env["PERSONA_MOCK_HUB"] else { throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)") }
        continueAfterFailure = false
        _ = try Data(contentsOf: URL(string: hub + "/mock/reset")!)
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-tab", "dev", "-openAgentsFresh", "YES", "-ui.style", style]
        app.launchPersona()

        // The Needs You cards fill the first screen: the list's own Terminal / Lark Group filters bring those rows up.
        for (filter, label) in [("terminal", "终端"), ("feishu", "飞书群")] {
            if filter == "feishu" && env["PERSONA_BOTMUX"] == nil { continue }
            let chip = app.buttons["agents.filter." + filter]
            XCTAssertTrue(chip.waitForExistence(timeout: 20), "no \(label) filter:\n\(app.debugDescription)")
            chip.tap()
            let tags = app.descendants(matching: .any).matching(identifier: "agents.row.source")
            XCTAssertTrue(tags.firstMatch.waitForExistence(timeout: 10), "no \(label) tag in the list:\n\(app.debugDescription)")
            sleep(1)
            checkTags(app, label)
            shot(app, "\(style)-\(filter)")
            chip.tap() // off again
        }
    }

    /// Every tag on screen is out of its row's badge column and in the row's right half: at its end, under the time.
    @MainActor private func checkTags(_ app: XCUIApplication, _ label: String) {
        let rows = app.descendants(matching: .any).matching(identifier: "agents.row").allElementsBoundByIndex.map(\.frame)
        let screen = app.windows.firstMatch.frame
        var checked = 0
        for tag in app.descendants(matching: .any).matching(identifier: "agents.row.source").allElementsBoundByIndex {
            let f = tag.frame
            guard !f.isEmpty, screen.contains(f), let row = rows.first(where: { $0.minY <= f.midY && f.midY <= $0.maxY }) else { continue }
            XCTAssertGreaterThanOrEqual(f.minX - row.minX, Self.badgeColumnEnd,
                                        "the \(label) tag at \(f) is in the badge's column of the row at \(row)")
            XCTAssertGreaterThan(f.minX, row.midX, "the \(label) tag at \(f) is not at the end of the row at \(row)")
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
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let look = env["PERSONA_SHOTS_APPEARANCE"] ?? "light"
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("source-tag-\(name)-\(look).png"))
    }
}
