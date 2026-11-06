import XCTest

/// A GitHub pull request opened from the inbox as GitHub's thread (#1055): the header (state, branches, labels),
/// the description, reviews, a comment on a line with its diff, the checks with a failure's log, and a comment (or a
/// quote reply) sent from the box at the bottom. A CI run, which is no thread, still opens as the text the hub read.
///   PORT=18797 MOCK_GITHUB_INBOX=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:PersonaUITests/GitHubThreadTests
/// Both styles (TEST_RUNNER_PERSONA_UI_STYLE=beautiful), dark and light.
final class GitHubThreadTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_GITHUB_INBOX=1 (PERSONA_MOCK_HUB)")
        }
    }

    @MainActor private func open(_ item: String) {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-inbox.kind", "", "-inbox.source", "", "-openInboxItem", item]
        app.launchPinned()
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "github \(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor private func el(_ id: String) -> XCUIElement {
        app.descendants(matching: .any).matching(identifier: id).firstMatch
    }

    @MainActor private func text(_ s: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
    }

    /// Scrolls the page up until e is on screen, in its upper half (for the shot), or (last) to the very end.
    @MainActor private func reach(_ e: XCUIElement, _ what: String, last: Bool = false) {
        for _ in 0..<8 where !(e.exists && e.isHittable) { app.swipeUp(velocity: .slow) }
        XCTAssertTrue(e.exists && e.isHittable, "\(what) is not on the page")
        let half = app.windows.firstMatch.frame.height / 2
        for _ in 0..<3 where last || e.frame.minY > half {
            app.swipeUp(velocity: .slow)
            if !e.isHittable { app.swipeDown(velocity: .slow); break }
        }
        sleep(1)
    }

    @MainActor private func sent() -> String {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        let deadline = Date.now.addingTimeInterval(8)
        while Date.now < deadline {
            if let data = try? Data(contentsOf: URL(string: hub + "/mock/sent")!), let s = String(data: data, encoding: .utf8),
               s.contains("github_comment") {
                return s
            }
            usleep(250_000)
        }
        return ""
    }

    /// The header, the description and the whole timeline, top to bottom.
    @MainActor func testPullRequestReadsAsGitHubsThread() {
        open("github:review12")
        XCTAssertTrue(el("github.header").waitForExistence(timeout: 20), "the pull request did not open as a thread:\n\(app.debugDescription)")
        XCTAssertTrue(text("Add retries to the example uploader").exists)
        XCTAssertTrue(text("example-org/example-repo · #12").exists, "no repository and number")
        XCTAssertTrue(el("github.state").label.contains("打开"), "no state: \(el("github.state").label)")
        XCTAssertTrue(text("从 feat/upload-retries 合并到 main").exists, "no branches")
        XCTAssertTrue(text("needs review").exists, "no labels")
        XCTAssertTrue(app.buttons["在 GitHub 打开"].exists)
        XCTAssertTrue(text("上传失败时按指数退避重试三次").exists, "no description")
        XCTAssertTrue(app.buttons["AI 草拟回复"].exists && app.buttons["交给 AI"].exists, "the inbox's bar is gone")
        sleep(2) // avatars
        shot("top")

        reach(text("uploader/retry.go:42"), "the comment on a line")
        XCTAssertTrue(text("要求修改").exists, "the review's state is not said")
        shot("review")

        reach(el("github.log"), "the failed check's log")
        XCTAssertTrue(text("3 项检查 · 1 项失败").exists, "no checks summary")
        XCTAssertTrue(text("--- FAIL: TestUploadRetries").exists, "the failure's log is not open")
        shot("checks")

        reach(el("github.entry.c2"), "the newest comment", last: true)
        XCTAssertTrue(text("批准了").exists)
        shot("bottom")
    }

    /// A comment from the box goes to the hub as github_comment and shows at the end; a quote reply quotes in Markdown.
    @MainActor func testCommentAndQuoteReply() {
        open("github:review12")
        XCTAssertTrue(el("github.header").waitForExistence(timeout: 20), "the pull request did not open as a thread")
        let newest = el("github.entry.c2") // drawn once scrolled to: the timeline is lazy
        reach(newest, "the newest comment")
        newest.press(forDuration: 1.0)
        let pick = app.buttons["引用回复"]
        XCTAssertTrue(pick.waitForExistence(timeout: 3), "no 引用回复 in the comment's menu")
        pick.tap()
        XCTAssertTrue(text("引用 alice").waitForExistence(timeout: 3), "the quote is not over the box")
        let box = el("thread.box")
        box.typeText("退避 1、2、4 秒可以")
        shot("quote")
        el("thread.send").tap()
        let s = sent()
        XCTAssertTrue(s.contains("> 请看一下重试的退避时间") && s.contains("退避 1、2、4 秒可以"), "not a quote reply: \(s)")
        XCTAssertTrue(text("退避 1、2、4 秒可以").waitForExistence(timeout: 5), "the comment is not on the page")
        XCTAssertFalse(text("引用 alice").exists, "the quote stayed over the box")
    }

    /// A CI run is no thread: the text the hub read, as before.
    @MainActor func testCIRunReadsAsText() {
        open("github:ci7")
        XCTAssertTrue(text("仓库 example-org/example-repo 的 CI").waitForExistence(timeout: 20), "the CI run's text did not show")
        XCTAssertFalse(el("github.header").exists)
    }
}
