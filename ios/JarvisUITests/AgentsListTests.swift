import XCTest

/// The Agents list with a lot of agents (scripts/mock_hub.py with MOCK_AGENTS=1: ~120 agents, a space with 3 needing
/// you, 35 finished-unread and 12 working): the quick filters, the compact rows, folding spaces, marking read.
///   TEST_RUNNER_JARVIS_AGENTS_HUB=http://127.0.0.1:<port>
/// TEST_RUNNER_JARVIS_SHOTS=<dir> also saves a screenshot of each step there (docs/testing.md「Agents 列表」).
final class AgentsListTests: XCTestCase {
    private var hub = ""
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let h = ProcessInfo.processInfo.environment["JARVIS_AGENTS_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py with MOCK_AGENTS=1 (JARVIS_AGENTS_HUB)")
        }
        hub = h
        _ = mock("reset") // every agent back to its state, nothing marked seen
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            add(attachment(XCTAttachment(screenshot: app.screenshot()), "failure"))
            add(attachment(XCTAttachment(string: app.debugDescription), "view tree"))
        }
        super.tearDown()
    }

    /// `fresh`: forget the list's saved look (cards, nothing folded, all machines, by space) first.
    /// `chat`: the chat left open last time ("<machine>/<session>"), which the app opens again over the list.
    private func launch(fresh: Bool = true, chat: String = "") {
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", chat, "-tab", "dev"] + (fresh ? ["-openAgentsFresh", "YES"] : [])
        app.launch()
        if chat.isEmpty { see(filter("all"), "the quick filters", timeout: 15) }
    }

    private func attachment(_ a: XCTAttachment, _ name: String) -> XCTAttachment {
        a.name = name
        a.lifetime = .keepAlways
        return a
    }

    private func shot(_ name: String) {
        let s = app.screenshot()
        add(attachment(XCTAttachment(screenshot: s), name))
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            let style = ProcessInfo.processInfo.environment["JARVIS_STYLE"].map { "-" + $0 } ?? ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + style + ".png"))
        }
    }

    @discardableResult
    private func see(_ e: XCUIElement, _ what: String, timeout: TimeInterval = 10) -> XCUIElement {
        if !e.waitForExistence(timeout: timeout) { XCTFail("\(what) not found") }
        return e
    }

    private func gone(_ e: XCUIElement, _ what: String) {
        let deadline = Date.now.addingTimeInterval(10)
        while e.exists, Date.now < deadline { usleep(200_000) }
        XCTAssertFalse(e.exists, what)
    }

    /// A row that can be below the fold (under the 需要你处理 cards and weather; the list builds rows lazily).
    @discardableResult private func reach(_ e: XCUIElement, _ what: String) -> XCUIElement {
        for _ in 0..<4 where !e.waitForExistence(timeout: 2) { app.swipeUp(velocity: .slow) }
        return see(e, what)
    }

    private func text(_ s: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label == %@", s)).firstMatch
    }

    private func button(_ s: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    private func filter(_ id: String) -> XCUIElement { app.buttons["agents.filter." + id] }

    /// A space's header (the button that folds it): "platform，已展开，…".
    private func header(_ space: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", space + "，")).firstMatch
    }

    private func mock(_ what: String) -> [String: Any] {
        guard let data = try? Data(contentsOf: URL(string: hub + "/mock/" + what)!),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else { return [:] }
        return obj
    }

    private func seen() -> [String] { mock("seen")["seen"] as? [String] ?? [] }

    private func wait(_ what: String, _ test: () -> Bool) {
        let deadline = Date.now.addingTimeInterval(10)
        while Date.now < deadline, !test() { usleep(300_000) }
        XCTAssertTrue(test(), what)
    }

    private func listMenu() -> XCUIElement {
        see(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "分组：")).firstMatch, "the list menu")
    }

    /// The n-th agent row (card or compact).
    private func row(_ n: Int, compact: Bool = false) -> XCUIElement {
        app.buttons.matching(identifier: compact ? "agents.compactRow" : "agents.row").element(boundBy: n)
    }

    /// Agent rows on screen now.
    private func visibleRows() -> Int {
        let screen = app.windows.firstMatch.frame
        return app.buttons.matching(NSPredicate(format: "identifier IN %@", ["agents.row", "agents.compactRow"]))
            .allElementsBoundByIndex.filter { c in
            c.exists && screen.intersects(c.frame)
                && c.frame.minY > 100
        }.count
    }

    // MARK: Quick filters

    /// 全部 / 需要你 / 未读 / 工作中 with their counts, one tap each; they add up with the machine menu.
    @MainActor func testQuickFilters() throws {
        launch()
        XCTAssertEqual(filter("needsYou").label, "需要你 6")
        XCTAssertEqual(filter("unread").label, "未读 47")
        XCTAssertEqual(filter("working").label, "工作中 19")
        see(text("给天气 App 加主屏幕小组件"), "a working agent under 全部")
        see(text("需要你处理"), "the cards that need you, under 全部")
        shot("agents-all")

        filter("unread").tap()
        XCTAssertTrue(filter("unread").isSelected)
        see(text("platform 任务 4"), "a finished-unread agent")
        gone(text("给天气 App 加主屏幕小组件"), "working agents leave under 未读")
        XCTAssertFalse(text("需要你处理").exists, "no cards that need you under 未读")
        see(button("全部标为已读"), "mark every one read, under 未读")
        shot("agents-unread")

        filter("working").tap()
        see(text("给天气 App 加主屏幕小组件"), "a working agent under 工作中")
        gone(text("platform 任务 4"), "finished ones leave under 工作中")

        filter("needsYou").tap()
        see(text("补齐网络层的单元测试"), "an agent that needs you")
        see(text("需要你处理"), "the cards that need you, under 需要你")
        gone(text("给天气 App 加主屏幕小组件"), "working agents leave under 需要你")

        // One machine: the counts are that machine's.
        listMenu().tap()
        see(app.buttons["MacBook"], "MacBook in the menu").tap()
        let unread = filter("unread")
        wait("未读 counts MacBook's only (\(unread.label))") { unread.label == "未读 5" }
        XCTAssertEqual(filter("needsYou").label, "需要你 1")
        filter("unread").tap()
        see(text("mobile 任务 1"), "MacBook's unread agent")
        gone(text("platform 任务 4"), "Mac Studio's agents leave")
        shot("example-machine")
        listMenu().tap()
        see(app.buttons["全部机器"], "全部机器").tap()
        wait("未读 counts every machine again") { filter("unread").label == "未读 47" }
        filter("all").tap()
        see(text("给天气 App 加主屏幕小组件"), "everything again under 全部")
    }

    // MARK: Compact rows

    /// 细列表 in the menu: one line per agent, several times as many on screen; remembered across launches.
    @MainActor func testCompactRows() throws {
        launch()
        filter("unread").tap()
        see(text("platform 任务 4"), "platform's rows")
        let cards = visibleRows()
        XCTAssertGreaterThan(cards, 0)

        listMenu().tap()
        see(app.buttons["细列表"], "细列表 in the menu").tap()
        see(row(0, compact: true), "compact rows")
        let compact = visibleRows()
        XCTAssertGreaterThanOrEqual(Double(compact), Double(cards) * 2.5, "compact \(compact) vs cards \(cards)")
        shot("agents-compact-unread")
        filter("all").tap()
        see(text("给天气 App 加主屏幕小组件"), "a working agent")
        shot("agents-compact-all")
        for _ in 0..<4 { app.swipeUp(velocity: .fast) }
        shot("agents-compact-scrolled")

        app.terminate()
        launch(fresh: false)
        see(row(0, compact: true), "still compact after a relaunch")
        listMenu().tap()
        see(app.buttons["卡片"], "卡片 in the menu").tap()
        see(row(0), "card rows again")
        XCTAssertFalse(row(0, compact: true).exists)
    }

    // MARK: Folding spaces

    /// Tapping a space's header folds it (its counts stay on the header) and back; remembered; 全部折叠 / 全部展开.
    @MainActor func testFoldingSpaces() throws {
        launch()
        let platform = see(header("platform"), "platform's header")
        XCTAssertTrue(platform.label.contains("已展开"), platform.label)
        reach(text("platform 任务 1"), "platform's first row")
        platform.tap()
        gone(text("platform 任务 1"), "platform's rows fold away")
        let folded = header("platform")
        XCTAssertTrue(folded.label.contains("已折叠"), folded.label)
        for n in ["3 个需要你", "12 个工作中", "35 个完成未看"] { XCTAssertTrue(folded.label.contains(n), "\(n) in \(folded.label)") }
        shot("agents-folded-one")

        app.terminate()
        launch(fresh: false)
        XCTAssertTrue(see(header("platform"), "platform's header").label.contains("已折叠"), "still folded after a relaunch")
        XCTAssertFalse(text("platform 任务 1").exists)

        listMenu().tap()
        see(app.buttons["全部折叠"], "全部折叠").tap()
        for space in ["weather", "platform", "infra"] {
            XCTAssertTrue(see(header(space), "\(space)'s header").label.contains("已折叠"), "\(space) folded")
        }
        gone(text("给天气 App 加主屏幕小组件"), "the pinned space folds too")
        shot("agents-folded-all")
        listMenu().tap()
        see(app.buttons["全部展开"], "全部展开").tap()
        reach(text("platform 任务 1"), "platform open again")
        XCTAssertTrue(header("platform").label.contains("已展开"))

        // A folded space still opens from its menu.
        for _ in 0..<4 { app.swipeDown(velocity: .fast) }
        header("weather").tap()
        button("weather 的更多操作").tap()
        see(app.buttons["打开空间"], "打开空间 in the space's menu").tap()
        see(app.navigationBars["weather"], "the space page")
    }

    // MARK: Many unread

    /// A space's ⋯ › 全部标为已读 marks its finished-unread agents seen in one go.
    @MainActor func testMarkSpaceRead() throws {
        launch()
        see(header("platform"), "platform's header")
        button("platform 的更多操作").tap()
        see(app.buttons["全部标为已读（35）"], "全部标为已读 with the count").tap()
        wait("the hub hears of 35 seen (\(seen().count))") { seen().count == 35 }
        let h = header("platform")
        wait("no finished-unread left on the header (\(h.label))") { !h.label.contains("完成未看") }
        XCTAssertEqual(filter("unread").label, "未读 12")
        shot("agents-space-read")
    }

    /// Under 未读 an opened agent stays where it was (seen now), so going down the list one by one works; switching
    /// filters drops it. 全部标为已读 empties the filter after asking.
    @MainActor func testWorkingThroughUnread() throws {
        launch()
        filter("unread").tap()
        let first = see(row(0), "the first unread row")
        let title = first.staticTexts.firstMatch.label
        first.tap()
        let back = see(app.buttons["返回"].firstMatch, "the chat")
        sleep(1) // the push has to finish first
        back.tap()
        see(text(title), "the opened agent stays in place under 未读")
        wait("the hub hears it was seen") { seen().count == 1 }
        XCTAssertEqual(filter("unread").label, "未读 46")
        let next = row(1)
        XCTAssertNotEqual(next.staticTexts.firstMatch.label, title, "the next one is below it")
        shot("agents-unread-walk")

        filter("all").tap()
        filter("unread").tap()
        gone(text(title), "a seen agent leaves once the filter is chosen again")

        button("全部标为已读").tap()
        see(app.buttons["全部标为已读（46）"], "the confirmation").tap()
        wait("the hub hears of every one (\(seen().count))") { seen().count == 47 }
        see(text("没有完成未看的会话"), "the empty filter")
        XCTAssertEqual(filter("unread").label, "未读 0")
        shot("agents-unread-empty")
    }

    // MARK: The filter bar stays under the bar

    /// How far the quick filters sit below the search field. The field must be there, open, in the navigation bar,
    /// and the bar drawn right under it: in the TestFlight 35 / 38 screenshot the field was gone and the bar sat far
    /// lower, blurred list between.
    private func gapAboveFilters(_ state: String) -> CGFloat {
        let bar = filter("all").frame
        let nav = app.navigationBars.firstMatch.frame
        let search = app.searchFields.firstMatch
        print("filters \(state): bar \(bar) nav \(nav) search \(search.exists ? search.frame : .zero)")
        XCTAssertTrue(search.exists, "\(state): no search field")
        guard search.exists else { return .infinity }
        let field = search.frame
        XCTAssertGreaterThan(field.height, 30, "\(state): the search field is folded")
        XCTAssertTrue(field.minY >= nav.minY && field.maxY <= nav.maxY + 1, "\(state): the search field is not in the navigation bar")
        XCTAssertGreaterThanOrEqual(bar.minY, field.maxY, "\(state): the bar is under the search field")
        XCTAssertTrue(drawn(bar), "\(state): the bar is not drawn where it is laid out")
        print("filters \(state): gap \(bar.minY - field.maxY)")
        return bar.minY - field.maxY
    }

    /// Whether the selected chip (全部: an accent-tinted capsule) is on screen at `frame`. Element frames come from
    /// layout: behind a chat restored at launch the bar was laid out under the search field but drawn ~170 pt lower
    /// (the whole top inset again), the TestFlight 35 / 38 screenshots; only the pixels show it.
    private func drawn(_ frame: CGRect) -> Bool {
        let image = app.screenshot().image
        guard let cg = image.cgImage else { return false }
        let scale = CGFloat(cg.width) / app.windows.firstMatch.frame.width
        let r = frame.insetBy(dx: 4, dy: 4)
        let w = Int(r.width * scale), h = Int(r.height * scale)
        var px = [UInt8](repeating: 0, count: w * h * 4)
        guard let ctx = CGContext(data: &px, width: w, height: h, bitsPerComponent: 8, bytesPerRow: w * 4,
                                  space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
        else { return false }
        ctx.draw(cg, in: CGRect(x: -r.minX * scale, y: -(CGFloat(cg.height) - r.maxY * scale), width: CGFloat(cg.width), height: CGFloat(cg.height)))
        var tinted = 0
        for i in stride(from: 0, to: px.count, by: 4) where Int(px[i + 2]) - Int(px[i]) > 20 { tinted += 1 }
        print("filters chip tinted \(tinted) of \(w * h)")
        return tinted * 2 > w * h
    }

    private func backToList() {
        _ = app.buttons["返回"].firstMatch.waitForExistence(timeout: 3)
        // The chat's own header (its back button is not in a navigation bar), full or folded.
        if let back = app.buttons.matching(NSPredicate(format: "label IN %@", ["返回", "Back"])).allElementsBoundByIndex
            .first(where: { $0.isHittable }) { back.tap() } else {
            let w = app.windows.firstMatch
            w.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5))
                .press(forDuration: 0.05, thenDragTo: w.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5)))
        }
        see(filter("all"), "the list again")
        sleep(1)
    }

    /// The bar hugs the search field in every state: scrolling (TestFlight 35 folded the field away and left the bar
    /// where it was), a chat and back, the app switcher, another tab, searching.
    @MainActor func testFilterBarHugsTheTop() throws {
        launch()
        let limit: CGFloat = 24 // 16 pt as laid out
        XCTAssertLessThan(gapAboveFilters("top"), limit, "before scrolling")
        shot("filters-top")
        // And it does not move with the list: the search field above it stays open (nothing folds away to leave a gap).
        let y = filter("all").frame.minY
        app.swipeUp(velocity: .slow)
        XCTAssertEqual(filter("all").frame.minY, y, accuracy: 1, "scrolled")
        XCTAssertLessThan(gapAboveFilters("scrolled"), limit, "after scrolling a screen")
        shot("filters-scrolled")
        // While the list still moves.
        app.swipeUp(velocity: .fast)
        XCTAssertEqual(filter("all").frame.minY, y, accuracy: 1, "moving")
        XCTAssertLessThan(gapAboveFilters("moving"), limit, "while scrolling")
        for _ in 0..<12 { app.swipeUp(velocity: .fast) }
        sleep(1)
        XCTAssertEqual(filter("all").frame.minY, y, accuracy: 1, "bottom")
        XCTAssertLessThan(gapAboveFilters("bottom"), limit, "at the bottom")
        shot("filters-bottom")
        for _ in 0..<14 { app.swipeDown(velocity: .fast) }
        sleep(1)
        XCTAssertLessThan(gapAboveFilters("back"), limit, "back at the top")
        // Pull to refresh.
        let w = app.windows.firstMatch
        w.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35))
            .press(forDuration: 0.05, thenDragTo: w.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.85)))
        XCTAssertEqual(filter("all").frame.minY, y, accuracy: 1, "refreshed")
        XCTAssertLessThan(gapAboveFilters("refreshed"), limit, "after pulling to refresh")
        shot("filters-refreshed")
        // A chat and back.
        row(0).tap()
        backToList()
        XCTAssertLessThan(gapAboveFilters("chat-back"), limit, "back from a chat")
        app.swipeUp(velocity: .slow)
        XCTAssertLessThan(gapAboveFilters("chat-back-scrolled"), limit, "back from a chat, scrolled")
        // Away and back (the app switcher's snapshot).
        XCUIDevice.shared.press(.home)
        sleep(2)
        app.activate()
        see(filter("all"), "the list after coming back")
        XCTAssertLessThan(gapAboveFilters("foreground"), limit, "back in the foreground")
        // Another tab and back.
        app.tabBars.buttons["Jarvis"].tap()
        sleep(1)
        app.tabBars.buttons["Agents"].tap()
        see(filter("all"), "the list after switching tabs")
        XCTAssertLessThan(gapAboveFilters("tabs"), limit, "after switching tabs")
        shot("filters-after-tabs")
        // Searching.
        let search = app.searchFields.firstMatch
        search.tap()
        search.typeText("platform")
        sleep(1)
        XCTAssertLessThan(gapAboveFilters("searching"), limit, "while searching")
        shot("filters-searching")
        app.swipeUp(velocity: .slow)
        XCTAssertLessThan(gapAboveFilters("searching-scrolled"), limit, "searching, scrolled")
        shot("filters-searching-scrolled")
    }

    /// Opening the app on the chat left open: the list is first laid out under it. Back at the list, the search field
    /// and the bar are where they always are.
    @MainActor func testFilterBarHugsTheTopBehindARestoredChat() throws {
        let key = try XCTUnwrap(firstAgentKey())
        launch(chat: key)
        see(app.buttons["更多"].firstMatch, "the restored chat", timeout: 15)
        backToList()
        XCTAssertLessThan(gapAboveFilters("restored-back"), 24, "back from the restored chat")
        shot("filters-restored-back")
        app.swipeUp(velocity: .slow)
        XCTAssertLessThan(gapAboveFilters("restored-back-scrolled"), 24, "back from the restored chat, scrolled")
    }

    private func firstAgentKey() -> String? {
        guard let data = try? Data(contentsOf: URL(string: hub + "/api/v1/sessions")!),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let s = (obj["sessions"] as? [[String: Any]])?.first(where: { $0["assistant"] as? Bool != true }),
              let m = s["machine_id"] as? String, let id = s["session_id"] as? String else { return nil }
        return m + "/" + id
    }

    // MARK: Long list

    /// Scrolling ~120 agents and switching the look: hitches in the test report (no hard limit on a simulator).
    @MainActor func testScrollingTheLongList() throws {
        launch()
        listMenu().tap()
        see(app.buttons["全部展开"].exists ? app.buttons["全部展开"] : app.buttons["全部折叠"], "the fold item")
        app.buttons["细列表"].tap()
        let list = app.collectionViews.firstMatch
        measure(metrics: [XCTOSSignpostMetric.scrollingAndDecelerationMetric]) {
            list.swipeUp(velocity: .fast)
            list.swipeDown(velocity: .fast)
        }
    }
}
