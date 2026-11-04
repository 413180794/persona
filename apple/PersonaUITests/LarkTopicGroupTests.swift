import XCTest

/// A Feishu topic group lists its topics as cards, as lark-connector does (#1087): three topics in a row from one bot each
/// keep their own card with the name and time (a chat runs them together under one name), a long topic folds behind
/// 展开 (what it hides takes no taps), a tap on a topic opens its thread as 话题详情 from the group, and a tap on a link
/// in a topic opens the link only. A recalled or edited topic follows 风灵月影 (防撤回, 编辑历史) as a chat's row does.
/// Against scripts/mock/mock_hub.py with MOCK_LARK_TOPIC=1 (JARVIS_MOCK_HUB); TEST_RUNNER_JARVIS_SHOTS=<dir> keeps a
/// screenshot of each step, light and dark.
final class LarkTopicGroupTests: XCTestCase {
    private var hub: String!
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_LARK_TOPIC=1 (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor func testEachTopicIsItsOwnCard() {
        for scheme in ["light", "dark"] {
            open(scheme: scheme)
            shot("topics-\(scheme)")
            // 401–403: the release bot, two minutes apart. Each card has the name and a time of its own.
            for text in ["灰度 10% 已开始", "v2.4 发版说明", "v2.4 构建通过"] {
                let card = topic(text)
                XCTAssertTrue(card.staticTexts["发布机器人"].exists, "\(text): no name on its card:\n\(card.debugDescription)")
                XCTAssertEqual(card.staticTexts.matching(NSPredicate(format: "label MATCHES %@", #".*\d{2}:\d{2}$"#)).count, 1,
                               "\(text): no time on its card:\n\(card.debugDescription)")
            }
            shot("topics-bot-\(scheme)")
            app.terminate()
        }
    }

    /// 风灵月影 on a topic card as on a chat's row (LarkMessageDisplayTests): a recalled topic says so, keeps what it
    /// said to itself and has no menu to copy it, until 防撤回 is on and lark-connector kept it; 已编辑 opens the old versions
    /// once 编辑历史 is on.
    @MainActor func testRecalledAndEditedTopicsFollowTheDisplaySettings() async throws {
        try await display("anti_recall", false)
        try await display("show_edit_history", false)
        open(scheme: "light")
        let gone = topic("小周 撤回了一条消息")
        XCTAssertFalse(app.staticTexts["不能展示的话题原文"].exists, "a recalled topic shows what it said")
        gone.staticTexts["小周 撤回了一条消息"].press(forDuration: 1)
        XCTAssertFalse(app.buttons["拷贝"].waitForExistence(timeout: 2), "a recalled topic has a menu to copy what it said")
        noReactionsOrReplies(topic("阿杰 撤回了一条消息"))
        XCTAssertFalse(app.staticTexts["已留存的话题原文：周会改到周五下午"].exists, "防撤回 is off, yet the kept topic shows")
        let edited = topic("周四上架")
        XCTAssertTrue(edited.staticTexts["已编辑"].exists, "no 已编辑 on the edited topic:\n\(edited.debugDescription)")
        XCTAssertFalse(edited.buttons["查看编辑历史"].exists, "编辑历史 is off, yet 已编辑 opens it")
        shot("topics-recalled-off")

        try await display("anti_recall", true)
        try await display("show_edit_history", true)
        let kept = app.staticTexts["已留存的话题原文：周会改到周五下午"]
        XCTAssertTrue(kept.waitForExistence(timeout: 15), "防撤回 is on, yet the kept topic does not show:\n\(app.debugDescription)")
        XCTAssertTrue(topic("已留存的话题原文").staticTexts["已撤回"].exists, "a kept recalled topic does not say 已撤回")
        noReactionsOrReplies(topic("已留存的话题原文"))
        XCTAssertTrue(topic("小周 撤回了一条消息").exists, "a recalled topic lark-connector did not keep lost its notice")
        XCTAssertFalse(app.staticTexts["不能展示的话题原文"].exists, "a recalled topic lark-connector did not keep shows its text")
        let history = topic("周四上架").buttons["查看编辑历史"]
        XCTAssertTrue(history.waitForExistence(timeout: 10), "编辑历史 is on, yet 已编辑 is not a button")
        shot("topics-recalled-on")
        history.tap()
        XCTAssertTrue(app.staticTexts["周三上架"].waitForExistence(timeout: 5), "the old version is not in 编辑历史:\n\(app.debugDescription)")
        app.buttons["完成"].firstMatch.tap()
        app.terminate()
        try await display("anti_recall", false)
        try await display("show_edit_history", false)
    }

    @MainActor func testALongTopicFoldsBehindShowMore() {
        open(scheme: "light")
        let card = topic("v2.4 发版说明")
        let fold = card.buttons["lark.topic.fold"]
        XCTAssertTrue(fold.exists, "the long topic has no 展开:\n\(card.debugDescription)")
        XCTAssertEqual(fold.label, "展开")
        let folded = card.frame.height
        shot("topic-folded")
        // What the fold hides takes no taps: in the card's foot, clear of the fold's edge and of the card's own buttons
        // (a tap a little off one still counts for it), a tap opens nothing.
        center(fold)
        let replies = card.buttons.matching(NSPredicate(format: "label CONTAINS %@", "条回复")).firstMatch
        point(x: card.frame.midX, y: (replies.frame.maxY + card.frame.maxY) / 2).tap()
        sleep(1)
        XCTAssertFalse(app.navigationBars["话题详情"].exists, "a tap under the fold opened the topic: what the fold hides took it")
        // 展开 takes a whole tap target, not just its pill: a tap near its top edge opens the fold, not the topic.
        fold.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.1)).tap()
        let open = card.buttons["lark.topic.fold"]
        XCTAssertTrue(open.waitForExistence(timeout: 3) && open.label == "收起", "展开 did not turn into 收起")
        XCTAssertGreaterThan(card.frame.height, folded + 100, "the topic did not open: \(folded) → \(card.frame.height)")
        for _ in 0..<3 where !open.isHittable { center(open) }
        shot("topic-open")
        open.tap()
        XCTAssertEqual(card.buttons["lark.topic.fold"].label, "展开")
        // Short topics have no fold.
        XCTAssertFalse(topic("v2.4 构建通过").buttons["lark.topic.fold"].exists)
    }

    /// Opened before (its topics cached, all on screen from the start), the group still opens a long topic: in a lazy
    /// stack the app was never idle again after 展开 (`LarkChatStack`).
    @MainActor func testALongTopicOpensInAGroupOpenedBefore() {
        open(scheme: "light")
        sleep(4) // the topics cached
        app.terminate()
        open(scheme: "light")
        let card = topic("v2.4 发版说明")
        let fold = card.buttons["lark.topic.fold"]
        XCTAssertTrue(fold.exists, "the long topic is not folded:\n\(card.debugDescription)")
        fold.tap()
        let opened = NSPredicate { _, _ in fold.exists && fold.label == "收起" }
        XCTAssertEqual(XCTWaiter.wait(for: [expectation(for: opened, evaluatedWith: nil)], timeout: 10), .completed,
                       "展开 did not open the topic (the app hung?)")
    }

    @MainActor func testATapOnATopicOpensItsThread() {
        open(scheme: "light")
        let question = app.staticTexts["下周三的版本冻结时间定了吗？我这边还有两个需求在提测"]
        reach(question)
        // A long press has the chat's menu; closed, it leaves the tap to open the topic working (a tap gesture and a
        // context menu on one view).
        question.press(forDuration: 1)
        XCTAssertTrue(app.buttons["拷贝"].waitForExistence(timeout: 5), "no menu on a long press:\n\(app.debugDescription)")
        shot("topic-menu")
        // Outside the menu, under the status bar (a tap on an iPhone's status bar does not close it).
        point(x: app.frame.midX, y: app.frame.height * 0.12).tap()
        XCTAssertTrue(app.buttons["拷贝"].waitForNonExistence(timeout: 5), "the menu did not close")
        question.tap()
        XCTAssertTrue(app.navigationBars["话题详情"].waitForExistence(timeout: 5), "the thread did not open:\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["来自 版本发布"].exists, "the thread does not say which group it is from")
        XCTAssertTrue(app.staticTexts["周二晚上 8 点冻结，之后只收修复"].waitForExistence(timeout: 10), "no replies in the thread")
        shot("topic-thread")
        app.buttons["完成"].tap()
        XCTAssertTrue(app.navigationBars["话题详情"].waitForNonExistence(timeout: 5))
    }

    @MainActor func testALinkInATopicOpensOnlyTheLink() {
        open(scheme: "light")
        let link = topic("回归用例表在这里").links.firstMatch
        XCTAssertTrue(link.exists, "no link in the topic:\n\(app.debugDescription)")
        link.tap()
        // In the link sheet, or in the system's browser when there is no assistant to hand it to (FeishuFoundation.swift).
        if !app.buttons["link-close"].waitForExistence(timeout: 5) {
            XCTAssertTrue(app.wait(for: .runningBackground, timeout: 1), "the link did not open:\n\(app.debugDescription)")
            app.activate()
        }
        XCTAssertFalse(app.navigationBars["话题详情"].exists, "the link opened the thread too")
    }

    // MARK: -

    /// A recalled topic shows neither its reactions nor its replies, whatever 风灵月影 says (a chat's row neither).
    @MainActor private func noReactionsOrReplies(_ card: XCUIElement) {
        XCTAssertFalse(card.staticTexts["Tom、Mia"].exists, "a recalled topic shows its reactions:\n\(card.debugDescription)")
        XCTAssertFalse(card.buttons["1 条回复"].exists, "a recalled topic shows its replies:\n\(card.debugDescription)")
    }

    /// Sets one of 风灵月影 on the mock hub, as the settings page does; the hub tells the app, which reads them again.
    private func display(_ key: String, _ on: Bool) async throws {
        let url = URL(string: hub + "/api/v1/lark/message-display")!
        let (data, _) = try await URLSession.shared.data(from: url)
        let now = try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
        guard now[key] as? Bool != on else { return }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try JSONSerialization.data(withJSONObject: ["account_id": now["account_id"] ?? "", "key": key, "value": on, "expected": !on])
        let (_, response) = try await URLSession.shared.data(for: req)
        XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200, "the mock hub did not take \(key) = \(on)")
    }

    @MainActor private func open(scheme: String) {
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-openInboxItem", "lark:f4:405", "-ui.scheme", scheme]
        app.launchPinned()
        XCTAssertTrue(topics.firstMatch.waitForExistence(timeout: 20), "the topic group did not open:\n\(app.debugDescription)")
        sleep(1)
    }

    @MainActor private var topics: XCUIElementQuery { app.descendants(matching: .any).matching(identifier: "lark.topic") }

    /// The card of the topic whose text starts with `text`, scrolled on screen.
    @MainActor private func topic(_ text: String) -> XCUIElement {
        let card = topics.containing(NSPredicate(format: "label BEGINSWITH %@", text)).firstMatch
        reach(card)
        XCTAssertTrue(card.exists, "no card for \(text):\n\(app.debugDescription)")
        return card
    }

    /// Scrolls until `element` is on screen: up first (the list opens at the newest topic), then back down.
    @MainActor private func reach(_ element: XCUIElement) {
        let list = app.scrollViews.firstMatch
        for _ in 0..<8 where !(element.exists && element.isHittable) { list.swipeDown() }
        for _ in 0..<12 where !(element.exists && element.isHittable) { list.swipeUp() }
        sleep(1)
    }

    /// Moves the list so `element` sits mid-screen: slowly, and held at the end so it does not fling.
    @MainActor private func center(_ element: XCUIElement) {
        let mid = app.frame.midY
        let from = point(x: app.frame.midX, y: mid)
        let dy = max(80 - mid, min(mid - 80, mid - element.frame.midY))
        guard abs(dy) > 40 else { return } // a drag that short would be a tap
        from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 0, dy: dy)), withVelocity: .slow, thenHoldForDuration: 0.3)
        sleep(1)
    }

    /// A point on the screen.
    @MainActor private func point(x: CGFloat, y: CGFloat) -> XCUICoordinate {
        app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: x, dy: y))
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
}
