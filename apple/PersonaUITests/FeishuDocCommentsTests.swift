import XCTest

/// Commenting in the Feishu document reader (#1290), end to end against scripts/mock/mock_hub.py started with
/// MOCK_DOC_COMMENTS=1 (PERSONA_MOCK_HUB): typing @ finds people and the comment goes out mentioning the one picked;
/// Reply answers that thread; Resolve moves a thread to Resolved and Reopen brings it back. What the plugin's routes got is
/// read from the mock (GET /mock/doc-comments), so the test checks what was sent, not only what shows.
final class FeishuDocCommentsTests: XCTestCase {
    private var hub: String!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_DOC_COMMENTS=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let appearance = env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/doc-comments-\(UITestStyle.name)-\(appearance)-\(name).png"))
    }

    /// What the mock's comment routes were sent, in order.
    private func writes() throws -> [[String: Any]] {
        var request = URLRequest(url: URL(string: hub + "/mock/doc-comments")!)
        request.setValue("Bearer mock", forHTTPHeaderField: "Authorization")
        let done = expectation(description: "writes")
        var body = Data()
        URLSession.shared.dataTask(with: request) { data, _, _ in body = data ?? Data(); done.fulfill() }.resume()
        wait(for: [done], timeout: 10)
        let json = try JSONSerialization.jsonObject(with: body) as? [String: Any]
        return json?["writes"] as? [[String: Any]] ?? []
    }

    @MainActor func testMentionReplyResolveAndReopen() throws {
        let before = try writes().count
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "studio/a1f3c2d4-0000-4000-8000-000000000003",
                               "-openLinkURL", "https://example.feishu.cn/docx/AbC123", "-showFeishuDocSheet", "comments"]
        app.launchPinned()

        let box = app.textFields["docComments.box"]
        XCTAssertTrue(box.waitForExistence(timeout: 30), "the comments sheet has no box:\n\(app.debugDescription)")
        try shot(app, "list")

        // @ finds people by name; picking writes `@name `, and the comment on the document goes out mentioning them.
        box.tap()
        box.typeText("@李") // the simulator's Chinese keyboard takes a typed space for choosing a word: none before the @
        let picker = app.descendants(matching: .any)["lark.mentionPicker"]
        XCTAssertTrue(picker.waitForExistence(timeout: 5), "@ found no one; the box holds \(box.value ?? "nil")")
        try shot(app, "mention")
        app.buttons["@李雷"].tap()
        XCTAssertTrue(picker.waitForNonExistence(timeout: 3))
        box.typeText("看一下深色模式")
        app.buttons["docComments.send"].tap()
        XCTAssertTrue(app.staticTexts["@李雷 看一下深色模式"].waitForExistence(timeout: 10), "the new comment did not show:\n\(app.debugDescription)")
        var sent = try writes().dropFirst(before)
        XCTAssertEqual(sent.first?["path"] as? String, "/api/v1/lark/documents/comments")
        let body = sent.first?["body"] as? [String: Any]
        XCTAssertEqual(body?["content"] as? String, "@李雷 看一下深色模式")
        XCTAssertEqual((body?["mentions"] as? [[String: Any]])?.first?["user_id"] as? String, "ou_mock_ll")
        XCTAssertNotNil(body?["client_id"] as? String)

        // Reply to a thread: the box says to whom, and the words go to that thread.
        app.buttons["docComments.reply.c2"].tap()
        XCTAssertTrue(app.descendants(matching: .any)["lark.replyBar"].waitForExistence(timeout: 3), "no 回复 bar")
        try shot(app, "replying")
        app.textFields["docComments.box"].typeText("已补，在第二页")
        app.buttons["docComments.send"].tap()
        XCTAssertTrue(app.staticTexts["已补，在第二页"].waitForExistence(timeout: 10), "the reply did not show")
        XCTAssertTrue(app.descendants(matching: .any)["lark.replyBar"].waitForNonExistence(timeout: 3), "the box still answers the thread")
        sent = try writes().dropFirst(before)
        XCTAssertEqual(sent.dropFirst().first?["path"] as? String, "/api/v1/lark/documents/comments/c2/replies")

        // Resolve moves the thread to Resolved; there Reopen brings it back.
        app.buttons["docComments.resolve.c1"].tap()
        XCTAssertTrue(app.buttons["docComments.resolve.c1"].waitForNonExistence(timeout: 10), "the thread stayed open")
        app.buttons["已解决 2"].tap()
        let reopen = app.buttons["docComments.resolve.c1"]
        XCTAssertTrue(reopen.waitForExistence(timeout: 5), "the thread is not under 已解决")
        try shot(app, "resolved")
        reopen.tap()
        XCTAssertTrue(reopen.waitForNonExistence(timeout: 10), "the thread was not reopened")
        sent = try writes().dropFirst(before)
        XCTAssertEqual(sent.map { $0["path"] as? String ?? "" }.suffix(2),
                       ["/api/v1/lark/documents/comments/c1/resolve", "/api/v1/lark/documents/comments/c1/resolve"])
        XCTAssertEqual(sent.suffix(2).map { ($0["body"] as? [String: Any])?["resolved"] as? Bool }, [true, false])
    }

    /// Comment on This: a long press on a paragraph offers it (with Copy, the text selection's place), the comments open with
    /// the box on that passage, and the new comment goes out quoting it.
    @MainActor func testCommentOnAPassage() throws {
        let before = try writes().count
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "studio/a1f3c2d4-0000-4000-8000-000000000003",
                               "-openLinkURL", "https://example.feishu.cn/docx/AbC123"]
        app.launchPinned()

        let paragraph = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "这一版统一了圆角和描边")).firstMatch
        XCTAssertTrue(paragraph.waitForExistence(timeout: 30), "the document did not open:\n\(app.debugDescription)")
        paragraph.press(forDuration: 1.2)
        let commentHere = app.buttons["评论这段"]
        XCTAssertTrue(commentHere.waitForExistence(timeout: 5), "no 评论这段 in the long press")
        XCTAssertTrue(app.buttons["拷贝"].exists && app.buttons["选一段评论…"].exists)
        try shot(app, "passage-menu")
        commentHere.tap()

        let bar = app.descendants(matching: .any)["docComments.box.quote"]
        XCTAssertTrue(bar.waitForExistence(timeout: 10), "the box is not on the passage")
        try shot(app, "passage-box")
        let box = app.textFields["docComments.box"]
        box.tap()
        box.typeText("ok")
        app.buttons["docComments.send"].tap()
        XCTAssertTrue(bar.waitForNonExistence(timeout: 10), "the box still comments on the passage")
        let sent = try writes().dropFirst(before)
        let body = sent.last?["body"] as? [String: Any]
        XCTAssertEqual(sent.last?["path"] as? String, "/api/v1/lark/documents/comments")
        XCTAssertEqual(body?["quote"] as? String, "这一版统一了圆角和描边，深色模式单独调了对比度。")
        XCTAssertEqual(body?["content"] as? String, "ok")
        try shot(app, "passage-sent")
    }
}
