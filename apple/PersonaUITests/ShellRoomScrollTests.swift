import XCTest

/// The shell's status row opening and folding over the Agents list, frame by frame (`probe.agents-scroll`'s `room`,
/// docs/ios-state.md「探针」): the search field stays as it was, pulled out or folded, and the rows move by the row's
/// room without a jump. The row used to take its room through the navigation stack's safe area: folding, the list read
/// an inset and offset of 0 for 100–200 ms, and now and then (2 in 12) the navigation bar took that for a scroll and
/// folded the search pulled out of it; it came back 0.6 s later, a visible flicker (#413).
///
/// Known to fail until #413 is fixed (what was tried, frame by frame, is in the issue): it runs only with
/// `TEST_RUNNER_JARVIS_KNOWN_ISSUES=1`, outside the acceptance runs.
final class ShellRoomScrollTests: XCTestCase {
    private var app: XCUIApplication!

    struct Place: Decodable {
        var nav, search, inset, row, container, safe: Double
    }

    struct Span: Decodable {
        var min, max: Double
        var size: Double { max - min }
    }

    struct Trip: Decodable {
        var n, frames: Int
        var row, nav, search, inset, container, safe: Span
        var rowStep: Double
        var to: Double?
    }

    struct Scroll: Decodable {
        var now: Place?
        var room: Trip?
    }

    /// The most a row may move in one frame: the room's animation moves it a few points a frame; the flicker's jumps
    /// were the whole inset.
    private static let step = 12.0
    /// Opens and folds per search state: the flicker came 2 times in 12.
    private static let cycles = 6

    override func setUpWithError() throws {
        continueAfterFailure = true
        guard ProcessInfo.processInfo.environment["JARVIS_KNOWN_ISSUES"] == "1" else {
            throw XCTSkip("fails until #413 is fixed: TEST_RUNNER_JARVIS_KNOWN_ISSUES=1 runs it")
        }
    }

    @MainActor func testTheSearchPulledOutStaysThroughTheRow() { watch(pulledOut: true) }

    @MainActor func testTheSearchFoldedStaysFoldedThroughTheRow() { watch(pulledOut: false) }

    @MainActor private func watch(pulledOut: Bool) {
        app = XCUIApplication()
        // Fine for 5 s, 连接中… for 3 s, again and again.
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "NO", "-app.tab", "chats", "-tab", "dev", "-dev.openChat", "",
                               "-openAgentsFresh", "YES", "-probes", "YES", "-showConnectionCycle", "connecting:5:3"]
        app.launchPinned()
        defer { app.terminate() }
        XCTAssertTrue(app.buttons.matching(identifier: "agents.row").firstMatch.waitForExistence(timeout: 15), "no agent in the list")
        sleep(2) // the search field folds itself away once
        if pulledOut { app.revealAgentsSearch() }
        let search = pulledOut ? "pulled out" : "folded"
        // Where the list rests with the row folded: once a fold is over, everything is back there.
        var rest: Place?
        var seen = scroll()?.room?.n ?? 0
        var trips = 0
        let start = Date()
        while trips < Self.cycles * 2, Date().timeIntervalSince(start) < Double(Self.cycles) * 8 + 30 {
            usleep(200_000)
            guard let s = scroll(), let now = s.now else { continue }
            if app.shellValue("status") == "none", s.room?.n == seen, rest == nil { rest = now }
            guard let t = s.room, t.n > seen else { continue }
            seen = t.n
            trips += 1
            let folded = (t.to ?? 0) == 0
            let what = "search \(search), \(folded ? "fold" : "open") #\(trips) (\(t.frames) frames)"
            print("room trip \(what): row \(t.row.min)…\(t.row.max) step \(t.rowStep), nav \(t.nav.min)…\(t.nav.max), "
                  + "search \(t.search.min)…\(t.search.max), inset \(t.inset.min)…\(t.inset.max), "
                  + "container \(t.container.min)…\(t.container.max), safe \(t.safe.min)…\(t.safe.max); now \(now)")
            XCTAssertGreaterThan(t.frames, 10, "too few frames watched (\(what))")
            XCTAssertEqual(t.search.size, 0, "the search field changed: \(t.search.min)…\(t.search.max) (\(what))")
            if pulledOut {
                XCTAssertGreaterThan(t.search.min, 0, "the search field folded (\(what))")
            } else {
                XCTAssertEqual(t.search.max, 0, "the search field came out (\(what))")
            }
            XCTAssertLessThanOrEqual(t.rowStep, Self.step, "the rows jumped \(t.rowStep) pt in one frame (\(what))")
            XCTAssertEqual(t.safe.size, 0, "the navigation container's safe area moved: \(t.safe.min)…\(t.safe.max) (\(what))")
            // A fold is over: the list is where it rested.
            if folded, let rest, app.shellValue("status") == "none" {
                XCTAssertEqual(now.row, rest.row, accuracy: 1, "the list is not back where it rested (\(what))")
                XCTAssertEqual(now.search, rest.search, accuracy: 0.5, "the search field is not back (\(what))")
            }
        }
        XCTAssertEqual(trips, Self.cycles * 2, "the row opened and folded \(trips) times, not \(Self.cycles * 2) (search \(search))")
    }

    @MainActor private func scroll() -> Scroll? { app.probe("agents-scroll", as: Scroll.self, page: "agents") }
}
