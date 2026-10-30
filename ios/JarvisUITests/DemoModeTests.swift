import XCTest

/// 体验模式 (docs/ios.md): from the pairing page into the app's own made-up hub (DemoHub, no server), every tab walked
/// through — Agents (filters, 细列表, a Claude and a Codex session, cards, a table, the terminal), the machines and
/// their 24-hour charts, Jarvis (sidebar, a conversation, 定时任务), the inbox (a Feishu chat, a mail) — and back out to
/// the pairing page from 设置. Needs no mock hub.
/// TEST_RUNNER_JARVIS_SHOTS=<dir> saves a screenshot of each step there (with TEST_RUNNER_JARVIS_STYLE in the name).
final class DemoModeTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            add(attachment(XCTAttachment(screenshot: app.screenshot()), "failure"))
            add(attachment(XCTAttachment(string: app.debugDescription), "view tree"))
        }
        super.tearDown()
    }

    private func attachment(_ a: XCTAttachment, _ name: String) -> XCTAttachment {
        a.name = name
        a.lifetime = .keepAlways
        return a
    }

    @MainActor private func shot(_ name: String) {
        let s = app.screenshot()
        add(attachment(XCTAttachment(screenshot: s), name))
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            let style = ProcessInfo.processInfo.environment["JARVIS_STYLE"].map { "-" + $0 } ?? ""
            try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("demo-" + name + style + ".png"))
        }
    }

    @MainActor @discardableResult
    private func see(_ e: XCUIElement, _ what: String, timeout: TimeInterval = 10) -> XCUIElement {
        if !e.waitForExistence(timeout: timeout) { XCTFail("\(what) not found") }
        return e
    }

    @MainActor private func text(_ s: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    @MainActor private func anything(_ s: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    /// No page asked the demo hub for something it does not have, and nothing failed out loud.
    @MainActor private func noErrors(_ page: String) {
        XCTAssertFalse(anything("体验模式里没有这项数据").exists, "\(page): the demo hub had nothing for a request")
        XCTAssertFalse(anything("连不上 hub").exists, "\(page): tried to reach a hub")
    }

    /// The page's own 返回 (a chat page has two, the folded header's and the hidden full one, both "hittable"): each
    /// in turn until the page goes, then the edge swipe.
    @MainActor private func back() {
        let all = app.buttons.matching(NSPredicate(format: "label == %@", "返回"))
        for b in all.allElementsBoundByIndex.reversed() where b.exists && b.isHittable {
            let frame = b.frame
            b.tap()
            sleep(1)
            if !b.exists || b.frame != frame || !b.isHittable { return }
        }
        let bar = app.navigationBars.buttons.firstMatch
        if bar.exists, bar.isHittable {
            bar.tap()
        } else {
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.5)).press(forDuration: 0.05, thenDragTo:
                app.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.5)))
        }
        sleep(1)
    }

    @MainActor private func tab(_ name: String) {
        app.tabBars.buttons[name].firstMatch.tap()
        sleep(1)
    }

    /// An agent's row in the Agents list (scrolling to it).
    @MainActor private func openAgent(_ title: String) {
        let list = app.buttons["agents.filter.all"]
        for _ in 0..<3 where !(list.exists && list.isHittable) { back() } // a back that did not take (the page was still settling)
        let row = text(title)
        // Clear of the filter bar and the tab bar (a row under the bar is "hittable" but the tap lands on a filter).
        func clear() -> Bool { row.exists && row.frame.minY > 260 && row.frame.maxY < app.frame.maxY - 140 }
        toTop()
        for _ in 0..<10 where !clear() { // a third of a screen at a time: a swipe would fling past it
            let up = !(row.exists && row.frame.minY <= 260)
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: up ? 0.7 : 0.4))
            from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: up ? 0.4 : 0.7)))
            sleep(1)
        }
        see(row, "the agent \(title)").tap()
        sleep(2)
    }

    @MainActor private func toTop() {
        for _ in 0..<4 { app.swipeDown() }
    }

    @MainActor func testWalkThroughAndLeave() throws {
        app = XCUIApplication()
        app.launchArguments = ["-openWelcome", "YES", "-dev.openChat", "", "-openAgentsFresh", "YES"]
        app.launch()

        // The pairing page: the way in, on one line.
        let demo = see(app.buttons["welcome.demo"], "体验模式 on the pairing page", timeout: 15)
        XCTAssertTrue(app.buttons["welcome.pair"].exists)
        shot("welcome")
        demo.tap()

        // Agents: spaces in every state, the quick filters, 细列表.
        see(text("给天气 App 加主屏幕小组件"), "the demo's agents", timeout: 10)
        XCTAssertTrue(text("weather").exists)
        noErrors("agents")
        shot("agents")
        see(app.buttons["agents.filter.unread"], "the 未读 filter").tap()
        see(text("博客迁移到 Astro"), "an unread agent")
        XCTAssertFalse(text("给天气 App 加主屏幕小组件").exists, "a working agent under 未读")
        shot("agents-unread")
        app.buttons["agents.filter.all"].tap()
        let menu = see(app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "分组：")).firstMatch, "the list menu")
        menu.tap()
        see(app.buttons["细列表"], "细列表 in the menu").tap()
        see(app.buttons.matching(identifier: "agents.compactRow").firstMatch, "compact rows")
        shot("agents-compact")
        menu.tap()
        see(app.buttons["卡片"], "卡片 in the menu").tap()
        sleep(1)

        // A Claude session: the work line, the to-do list, the edit; a message gets the demo's answer.
        openAgent("给天气 App 加主屏幕小组件")
        see(text("小组件的时间线已经能按小时刷新了"), "the Claude session's reply")
        noErrors("claude chat")
        shot("claude")
        let field = see(app.textFields.firstMatch, "the message field")
        field.tap()
        field.typeText("看一下进度")
        see(app.buttons["发送"], "send").tap()
        see(text("这是体验模式"), "the demo's answer to a message", timeout: 10)
        shot("claude-sent")

        // The terminal: the made-up Claude screen, its spinner ticking.
        app.swipeDown() // the keyboard away
        sleep(1)
        // The page's own bar: the compact copy that shows once the chat scrolled (the full one sits under the clock).
        let buttons = app.buttons.matching(identifier: "terminal")
        let bar = (0..<buttons.count).map { buttons.element(boundBy: $0) }.first { $0.isHittable && $0.frame.minY > 40 }
        guard let bar else { return XCTFail("no 终端 on the session page:\n\(app.debugDescription)") }
        bar.tap()
        see(app.descendants(matching: .any)["terminal-close"], "the terminal", timeout: 10)
        let tips = app.buttons["知道了"] // the terminal's tips, the first time
        if tips.waitForExistence(timeout: 3) { tips.tap() }
        sleep(2) // a few frames of the made-up screen
        shot("terminal")
        let close = app.descendants(matching: .any)["terminal-close"]
        if close.exists { close.tap() } else { back() }
        sleep(1)
        back()

        // A question card: answering it, the agent picks up (nothing is sent).
        openAgent("补齐网络层的单元测试")
        see(text("网络层的测试用哪个框架"), "the question card")
        shot("question")
        see(anything("Swift Testing"), "the recommended option").tap()
        let submit = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "提交")).firstMatch
        if submit.waitForExistence(timeout: 2), submit.isHittable { submit.tap() }
        see(text("收到，按你的选择接着做"), "the agent picking up after the answer", timeout: 10)
        back()

        // A plan card, a Codex session, a reply with a table and a link.
        openAgent("照片同步脚本支持 HEIC")
        see(anything("支持 HEIC"), "the plan card")
        shot("plan")
        back()
        openAgent("排查 /orders 接口变慢")
        see(text("N+1"), "the Codex session's reply")
        noErrors("codex chat")
        shot("codex")
        back()
        openAgent("整理待拍板事项")
        see(anything("小组件刷新频率"), "the table")
        shot("table")
        back()
        toTop()

        // Machines: the dashboard, one machine, its 24 hours.
        menu.tap()
        see(app.buttons["机器监控"], "机器监控 in the menu").tap()
        see(app.staticTexts["服务器"], "the dashboard")
        noErrors("dashboard")
        shot("dashboard")
        app.staticTexts["服务器"].tap()
        let range = see(app.segmentedControls["metric-range"], "the machine page")
        range.buttons["24 小时"].tap()
        see(app.descendants(matching: .any)["chart-CPU"], "the CPU chart")
        noErrors("machine")
        shot("machine-24h")
        back()
        back()

        // Jarvis: a conversation, the sidebar, 定时任务.
        tab("Jarvis")
        see(text("今天有"), "the main conversation", timeout: 10)
        noErrors("jarvis")
        shot("jarvis")
        see(app.buttons["历史对话"], "历史对话").tap()
        see(app.descendants(matching: .any)["assistant-sidebar"], "the sidebar")
        shot("jarvis-sidebar")
        app.swipeLeft()
        sleep(1)
        see(app.buttons["jarvis-avatar"], "the avatar").tap()
        see(app.buttons["详细信息"], "详细信息").tap()
        let routines = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "定时任务")).firstMatch
        for _ in 0..<4 where !(routines.exists && routines.isHittable) { app.swipeUp() }
        see(routines, "定时任务").tap()
        see(text("早报"), "the routines")
        shot("routines")
        back()
        back()

        // The inbox: a Feishu chat, a mail.
        tab("收件箱")
        see(text("周五的发布能赶上吗"), "the inbox", timeout: 10)
        noErrors("inbox")
        shot("inbox")
        text("周五的发布能赶上吗").tap()
        see(text("下午我要和客户确认时间"), "the Feishu chat")
        noErrors("feishu")
        shot("feishu")
        back()
        let mail = text("下周的分享能不能提前到周二")
        for _ in 0..<3 where !(mail.exists && mail.isHittable) { app.swipeUp() }
        mail.tap()
        see(text("周四我临时要出差"), "the mail")
        shot("mail")
        back()

        // 设置 says it is the demo; leaving it is the pairing page again, and stays so after a relaunch.
        tab("设置")
        see(text("体验模式 · 数据都是编造的"), "体验模式 in 设置")
        shot("settings")
        let leave = app.buttons["settings.leaveDemo"]
        for _ in 0..<3 where !(leave.exists && leave.isHittable) { app.swipeUp() }
        see(leave, "退出体验模式").tap()
        see(app.buttons["welcome.demo"], "the pairing page after leaving")
        app.terminate()
        app.launchArguments = ["-dev.openChat", ""]
        app.launch()
        see(app.buttons["welcome.demo"], "the pairing page after a relaunch", timeout: 15)
    }

    /// 体验模式 is remembered: a relaunch opens the demo again, not the pairing page.
    @MainActor func testStaysAcrossLaunches() throws {
        app = XCUIApplication()
        app.launchArguments = ["-openWelcome", "YES", "-dev.openChat", ""]
        app.launch()
        see(app.buttons["welcome.demo"], "体验模式 on the pairing page", timeout: 15).tap()
        see(text("给天气 App 加主屏幕小组件"), "the demo's agents")
        app.terminate()
        app.launchArguments = ["-dev.openChat", ""]
        app.launch()
        see(text("给天气 App 加主屏幕小组件"), "the demo after a relaunch", timeout: 15)
        tab("设置")
        let leave = app.buttons["settings.leaveDemo"]
        for _ in 0..<3 where !(leave.exists && leave.isHittable) { app.swipeUp() }
        see(leave, "退出体验模式").tap()
        see(app.buttons["welcome.demo"], "the pairing page after leaving")
    }
}
