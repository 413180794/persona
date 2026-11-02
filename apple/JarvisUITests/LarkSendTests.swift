import XCTest

/// Sending in a Feishu chat does not wait for Feishu (docs/ios.md「飞书消息渲染」): the message is on screen at once
/// with a spinner beside it, Feishu's own copy takes its place, and one that did not go out has a failed mark that offers
/// 重发; replies (quoted in the chat, or in a thread sheet) go the same way. The marks are found by their identifiers
/// (lark.outgoing.sending / .failed), not their wording. Against scripts/mock_hub.py
/// (JARVIS_MOCK_HUB), whose Feishu send takes 2 seconds and fails for a text with "#fail".
final class LarkSendTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func openChat() -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-inbox.kind", "mention", "-inbox.source", "", "-openInboxFirst", "YES"]
        app.launchPinned()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "The new icon set")).firstMatch.waitForExistence(timeout: 20),
                      "the 设计评审 chat did not open")
        return app
    }

    @MainActor private func send(_ app: XCUIApplication, _ text: String) {
        let field = app.textFields.matching(NSPredicate(format: "placeholderValue BEGINSWITH %@", "回复")).firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.tap()
        field.typeText(text)
        app.buttons["发送"].tap()
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name + ".png"))
        }
    }

    @MainActor func testMessageShowsAtOnceThenItsCopyTakesOver() {
        let app = openChat()
        let text = "下午三点前我看一下 " + UUID().uuidString.prefix(8)
        send(app, text)
        let sending = app.descendants(matching: .any).matching(identifier: "lark.outgoing.sending").firstMatch
        XCTAssertTrue(sending.waitForExistence(timeout: 1), "the message is not on screen while it goes")
        XCTAssertTrue(app.staticTexts[text].exists)
        let field = app.textFields.matching(NSPredicate(format: "placeholderValue BEGINSWITH %@", "回复")).firstMatch
        XCTAssertNotEqual(field.value as? String, text, "the box is cleared at once")
        shot(app, "lark-sending")
        XCTAssertTrue(sending.waitForNonExistence(timeout: 15), "it never went out")
        sleep(3) // the copy replaces it: one message with the text, not two
        shot(app, "lark-sent")
        let copies = app.staticTexts.matching(NSPredicate(format: "label == %@", text))
        XCTAssertEqual(copies.count, 1, copies.debugDescription)
    }

    @MainActor func testFailedMessageOffersRetry() {
        let app = openChat()
        send(app, "#fail 这条发不出去")
        let failed = app.buttons["lark.outgoing.failed"]
        XCTAssertTrue(failed.waitForExistence(timeout: 10), "a failed send says so")
        shot(app, "lark-failed")
        failed.tap()
        XCTAssertTrue(app.buttons["重发"].waitForExistence(timeout: 3))
        XCTAssertTrue(app.buttons["放回输入框"].exists)
        app.buttons["删除"].tap()
        XCTAssertTrue(failed.waitForNonExistence(timeout: 3))
    }

    /// The send marks sit beside the bubble, like Feishu: the failed mark left of the failed message on its line, and
    /// spinners while later ones go. Two more messages push the failed one up out from under the box for the shot.
    @MainActor func testSendMarksSitBesideTheBubble() {
        let app = openChat()
        let failedText = "#fail 旁边是标记 " + UUID().uuidString.prefix(6)
        send(app, failedText)
        let failed = app.buttons["lark.outgoing.failed"]
        XCTAssertTrue(failed.waitForExistence(timeout: 10), "a failed send has its mark")
        let bubble = app.staticTexts[failedText]
        XCTAssertTrue(bubble.exists)
        XCTAssertLessThanOrEqual(failed.frame.maxX, bubble.frame.minX, "the mark is left of the bubble")
        XCTAssertLessThan(abs(failed.frame.midY - bubble.frame.midY), 12, "on the bubble's line, not under it")
        send(app, "第二条 " + UUID().uuidString.prefix(6))
        send(app, "第三条 " + UUID().uuidString.prefix(6))
        let sending = app.descendants(matching: .any).matching(identifier: "lark.outgoing.sending")
        XCTAssertTrue(sending.firstMatch.waitForExistence(timeout: 2), "the ones going have spinners")
        shot(app, "lark-marks")
        XCTAssertTrue(failed.exists, "the failed one stays until it is dealt with")
    }

    /// 回复 in a message's long-press menu: a quote bar over the box, and the message goes out quoting it.
    @MainActor func testQuotedReplyShowsTheQuoteWhileItGoes() {
        let app = openChat()
        let target = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "我先看了一下")).firstMatch
        XCTAssertTrue(target.waitForExistence(timeout: 5))
        target.press(forDuration: 1.2)
        let reply = app.buttons["回复"]
        XCTAssertTrue(reply.waitForExistence(timeout: 3), "the long-press menu has 回复")
        reply.tap()
        let bar = app.staticTexts["回复 李雷"]
        XCTAssertTrue(bar.waitForExistence(timeout: 3), "the quote bar names who is answered")
        XCTAssertTrue(app.buttons["取消回复"].exists)
        shot(app, "lark-reply-bar")
        let text = "我来改 " + UUID().uuidString.prefix(8)
        send(app, text)
        // The mock takes 2 s to send: what shows within a second is the local row. (Its spinner can sit under the
        // keyboard, so the row is checked by its text and quote.)
        XCTAssertTrue(app.staticTexts[text].waitForExistence(timeout: 1), "the reply is not on screen while it goes")
        XCTAssertTrue(bar.waitForNonExistence(timeout: 2), "the quote bar goes with the message")
        let quotes = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "李雷：我先看了一下"))
        XCTAssertTrue(quotes.firstMatch.exists, "the sending row quotes the message")
        shot(app, "lark-reply-sending")
        sleep(6) // sent, and the copy replaces it: one reply, quoting the message
        let copies = app.staticTexts.matching(NSPredicate(format: "label == %@", text))
        XCTAssertEqual(copies.count, 1, copies.debugDescription)
        XCTAssertTrue(quotes.firstMatch.exists, "the copy quotes the message too")
    }

    /// What is typed and not sent is still in the box, quote and all, after the app is gone and back (LarkDrafts).
    /// In 周末徒步群, whose box no other test types into: a draft left by a failed run stays out of their way.
    @MainActor func testDraftOutlivesTheApp() {
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        func open() -> XCUIApplication {
            let app = XCUIApplication()
            app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-openInboxItem", "lark:f3:300"]
            app.launchPinned()
            return app
        }
        var app = open()
        let target = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "那我也带一个")).firstMatch
        XCTAssertTrue(target.waitForExistence(timeout: 20), "the 周末徒步群 chat did not open")
        // A draft a failed run left behind goes out first (to the mock), so the box starts empty.
        if app.buttons["lark.send"].isEnabled { app.buttons["lark.send"].tap() }
        target.press(forDuration: 1.2)
        XCTAssertTrue(app.buttons["回复"].waitForExistence(timeout: 3), "the long-press menu has 回复")
        app.buttons["回复"].tap()
        XCTAssertTrue(app.staticTexts["回复 Mia"].waitForExistence(timeout: 3))
        let text = "草稿" + UUID().uuidString.prefix(8)
        let box = app.textFields.matching(NSPredicate(format: "placeholderValue BEGINSWITH %@", "回复")).firstMatch
        XCTAssertTrue(box.waitForExistence(timeout: 5))
        box.typeText(text) // 回复 focused it
        sleep(3) // written a moment after the last keystroke
        app.terminate()

        app = open()
        let back = app.textFields.matching(NSPredicate(format: "value == %@", text)).firstMatch
        XCTAssertTrue(back.waitForExistence(timeout: 20), "the draft is back in the box")
        XCTAssertTrue(app.staticTexts["回复 Mia"].exists, "the quote comes back with it")
        shot(app, "lark-draft-back")

        // Sending it empties the box and drops the draft (the next run starts from an empty box).
        app.buttons["lark.send"].tap()
        XCTAssertTrue(back.waitForNonExistence(timeout: 3), "the box is cleared when the draft is sent")
        XCTAssertTrue(app.staticTexts["回复 Mia"].waitForNonExistence(timeout: 3))
        sleep(3)
    }

    /// The thread sheet has its own box: a reply there goes into the thread, shows at once, and its copy takes over.
    @MainActor func testThreadSheetRepliesInTheThread() {
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-openInboxItem", "lark:f3:300"]
        app.launchPinned()
        let thread = app.buttons.matching(NSPredicate(format: "label ENDSWITH %@", "条回复")).firstMatch
        XCTAssertTrue(thread.waitForExistence(timeout: 20), "the 周末徒步群 chat did not open at its thread")
        thread.tap()
        let field = app.textFields.matching(NSPredicate(format: "placeholderValue == %@", "回复话题")).firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 5), "the thread sheet has a box")
        XCTAssertTrue(app.staticTexts["60 条回复"].waitForExistence(timeout: 10), "the thread's latest page did not load")
        let text = "我也报名，坐地铁 " + UUID().uuidString.prefix(8)
        field.tap()
        field.typeText(text)
        shot(app, "lark-thread-box")
        app.buttons["lark.thread.send"].tap()
        XCTAssertTrue(app.staticTexts[text].waitForExistence(timeout: 1), "the reply is not on screen while it goes")
        shot(app, "lark-thread-sending")
        sleep(6) // sent, and the newest page brings its copy in its place
        XCTAssertEqual(app.staticTexts.matching(NSPredicate(format: "label == %@", text)).count, 1)
        shot(app, "lark-thread-sent")
    }
}

