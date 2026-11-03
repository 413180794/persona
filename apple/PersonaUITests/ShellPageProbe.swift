import XCTest

extension XCUIApplication {
    /// What the shell's strip shows, from the app's probe (`ShellPagerProbe`, DEBUG builds): the page it rests on
    /// (`assistant` / `agents`, `moving` while it slides), the page in front (what the switch shows) and the app's tab,
    /// e.g. "agents selected=agents tab=agents position=1.00 drawer=0 chatRoot=1 workRoot=1".
    /// Both pages stay built and the hidden one can be back in the accessibility tree (its lists are UIKit), so
    /// `exists` / `isHittable` of a page's controls do not say which page is drawn. This used to be read from the
    /// pixels of the 全部 chip (`agentsDrawn`), which broke when the chip moved into the list (56b2ead).
    var shellPage: String {
        (descendants(matching: .any)["shell-pager-probe"].firstMatch.value as? String) ?? "none"
    }

    /// One `key=value` of the probe's value (`room`, `status`, `opened`…).
    func shellValue(_ key: String) -> String? {
        shellPage.split(separator: " ").first { $0.hasPrefix(key + "=") }.map { String($0.dropFirst(key.count + 1)) }
    }

    /// The room the status row takes at the top (points): the bars and pages are that much lower.
    var shellRoom: CGFloat { CGFloat(Double(shellValue("room") ?? "0") ?? 0) }

    /// The shell's latest events (switch taps, routes, slides, settles), for a failure's message.
    var shellTrace: String {
        "[\(shellPage)] trace: " + descendants(matching: .any)["shell-pager-probe"].firstMatch.label
    }

    /// Whether Agents is what the strip rests on, with the switch and the app's tab on Agents too (TestFlight 70: the
    /// switch said Agents over the empty chat; 2026-11-02: the tab said Agents over 助手 and the switch's Agents did
    /// nothing. The page, the selection and the tab must agree).
    func agentsDrawn() -> Bool { shellPage.hasPrefix("agents selected=agents tab=agents ") }

    /// Whether 助手 is what the strip rests on, with the switch and the app's tab on 助手.
    func assistantDrawn() -> Bool { shellPage.hasPrefix("assistant selected=assistant tab=assistant ") }

    /// Waits (the pages slide) until the strip rests on Agents (`shown`) or on 助手.
    func waitAgentsDrawn(_ shown: Bool) -> Bool {
        let met = { shown ? self.agentsDrawn() : self.assistantDrawn() }
        var now = met()
        for _ in 0..<10 where !now {
            usleep(300_000)
            now = met()
        }
        return now
    }
}

/// The shell's switch as a finger uses it (2026-11-02: the user's taps on Agents did nothing): taps at the points of
/// 助手 and Agents, read once from the switch on 助手 at launch. Not the accessibility elements: the tree has offered a
/// hidden page's copy before (`ShellSwitchTests`), and a tap on an element is not where a thumb lands.
@MainActor struct ShellFinger {
    let app: XCUIApplication
    private let assistant: CGPoint
    private let agents: CGPoint

    /// The points as they are with the status row folded (`shellRoom` moves the bar down while it is open).
    init(_ app: XCUIApplication, file: StaticString = #filePath, line: UInt = #line) {
        self.app = app
        let a = app.buttons["shell-assistant"].firstMatch, g = app.buttons["shell-agents"].firstMatch
        XCTAssertTrue(a.waitForExistence(timeout: 15) && g.exists, "no 助手 | Agents switch", file: file, line: line)
        let room = app.shellRoom
        assistant = CGPoint(x: a.frame.midX, y: a.frame.midY - room)
        agents = CGPoint(x: g.frame.midX, y: g.frame.midY - room)
    }

    /// Taps the segment, no waiting (quick taps, taps while the strip slides): where it is now, below the status row
    /// if that is open.
    func tap(agents toAgents: Bool, room: CGFloat? = nil) {
        let p = toAgents ? agents : assistant
        let dy = room ?? app.shellRoom
        app.windows.firstMatch.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: p.x, dy: p.y + dy)).tap()
    }

    /// Taps the segment, then within `limit` seconds the strip must rest on that page with the switch and the app's
    /// tab on it too (docs/shell.md「及时」: the slide takes about 0.4 s; the rest is the probe's reads).
    @discardableResult
    func switchTo(agents toAgents: Bool, _ what: String, limit: TimeInterval = 1.5,
                  file: StaticString = #filePath, line: UInt = #line) -> Bool {
        tap(agents: toAgents)
        return app.expectShell(agents: toAgents, what, limit: limit, file: file, line: line)
    }
}

extension XCUIApplication {
    /// Within `limit` seconds the strip rests on Agents (or 助手) with the switch and the app's tab on it too; a failure
    /// says what the probe saw last and the shell's latest events.
    @MainActor @discardableResult
    func expectShell(agents: Bool, _ what: String, limit: TimeInterval = 1.5,
                     file: StaticString = #filePath, line: UInt = #line) -> Bool {
        let start = Date()
        let met = { agents ? self.agentsDrawn() : self.assistantDrawn() }
        var ok = met()
        while !ok, Date().timeIntervalSince(start) < limit {
            usleep(100_000)
            ok = met()
        }
        if !ok {
            let shot = XCTAttachment(screenshot: screenshot())
            shot.name = what
            shot.lifetime = .keepAlways
            XCTContext.runActivity(named: what) { $0.add(shot) }
            XCTFail("\(agents ? "Agents" : "助手") is not in front \(String(format: "%.1f", limit)) s after: \(what) \(shellTrace)",
                    file: file, line: line)
        }
        return ok
    }
}
