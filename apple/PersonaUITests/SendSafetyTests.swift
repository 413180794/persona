import XCTest

/// Answers and sends that must not land in a terminal that changed (docs/protocol/protocol.md「发消息」「终端对话框」), against
/// scripts/mock/mock_hub.py started with MOCK_SCREEN_DIALOG=1 MOCK_PROMPT_CHANGED=1 MOCK_SEND_REFUSE=input_draft
/// (TEST_RUNNER_PERSONA_SAFETY_HUB=http://127.0.0.1:<port>).
final class SendSafetyTests: XCTestCase {
    private let review = "a1f3c2d4-0000-4000-8000-000000000006"
    private let widget = "a1f3c2d4-0000-4000-8000-000000000001"

    private func hub() throws -> String {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_SAFETY_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_SAFETY_HUB)")
        }
        return hub
    }

    private func launch(_ hub: String, _ args: [String]) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair] + args
        app.launchPinned()
        return app
    }

    /// GET /mock/<what> until test says it has what it waits for.
    private func poll(_ hub: String, _ what: String, until test: ([[String: Any]]) -> Bool) -> [[String: Any]] {
        let deadline = Date.now.addingTimeInterval(10)
        var list: [[String: Any]] = []
        while Date.now < deadline {
            if let data = try? Data(contentsOf: URL(string: hub + "/mock/" + what)!),
               let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                list = obj[what] as? [[String: Any]] ?? []
                if test(list) { break }
            }
            usleep(300_000)
        }
        return list
    }

    /// A dialog the hub read is answered by option, never by guessed keys; a changed dialog says so.
    @MainActor func testScreenDialogAnswersByOption() throws {
        let hub = try hub()
        let app = launch(hub, ["-openSession", "macbook/" + review])
        let yes = app.buttons["Yes"]
        XCTAssertTrue(yes.waitForExistence(timeout: 15), "no dialog card:\n\(app.debugDescription)")
        yes.tap()
        let resolved = poll(hub, "resolved") { $0.contains { ($0["id"] as? String)?.hasPrefix("scr_") == true } }
        let r = try XCTUnwrap(resolved.last { ($0["id"] as? String)?.hasPrefix("scr_") == true }?["resolution"] as? [String: Any])
        XCTAssertEqual(r["option"] as? String, "1")
        XCTAssertNil(r["keys"], "keys are worked out on the computer, from the screen as it is then")
        let toast = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "对话框已经变了")).firstMatch
        XCTAssertTrue(toast.waitForExistence(timeout: 5), "no prompt_changed toast")
    }

    /// The computer's input box has a draft: the alert shows it, Clear and Send resends the same message with clear_draft.
    @MainActor func testDraftRefusalClearsAndSends() throws {
        let hub = try hub()
        let app = launch(hub, ["-openSession", "macbook/" + widget, "-openSendMessage", "把测试也跑一遍"])
        let clear = app.alerts.buttons["清空并发送"]
        XCTAssertTrue(clear.waitForExistence(timeout: 15), "no draft alert:\n\(app.debugDescription)")
        XCTAssertTrue(app.alerts.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "顺便把 README")).firstMatch.exists)
        clear.tap()
        let sent = poll(hub, "sent") { $0.contains { $0["clear_draft"] != nil } }
        let first = try XCTUnwrap(sent.first), retry = try XCTUnwrap(sent.last { $0["clear_draft"] != nil })
        XCTAssertEqual(retry["clear_draft"] as? String, "顺便把 README 也更新一下")
        XCTAssertEqual(retry["client_msg_id"] as? String, first["client_msg_id"] as? String, "a retry keeps its id")
    }
}
