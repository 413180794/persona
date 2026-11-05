import XCTest

/// 更多 › 全部标为已处理 / 全部忽略 (docs/inbox.md「全部标为已处理」「一键清理」): the menu says what they cover (the 飞书
/// filter here) and how many; one bulk request files them all, with their versions; the toast's 撤销 brings them back. Against
/// scripts/mock/mock_hub.py (JARVIS_MOCK_HUB), which keeps the bulk requests at /mock/inbox_bulk (each test counts the
/// ones it adds, so both run against one mock). `TEST_RUNNER_JARVIS_SHOTS=<dir>` also writes the screenshots there.
final class InboxMarkAllTests: XCTestCase {
    @MainActor func testMarkAllInTheFilterThenUndo() throws { try clean(entry: "全部标为已处理", action: "done", doneBy: "user",
                                                                      toast: { "已将 \($0) 件标为已处理" }, shotName: "inbox mark all") }

    @MainActor func testDismissAllInTheFilterThenUndo() throws { try clean(entry: "全部忽略", action: "dismiss", doneBy: "dismissed",
                                                                         toast: { "已忽略 \($0) 件" }, shotName: "inbox dismiss all") }

    @MainActor private func clean(entry: String, action: String, doneBy: String, toast: (Int) -> String, shotName: String) throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-inbox.kind", "", "-inbox.source", "feishu"]
        app.launchPinned()
        func shot(_ name: String) {
            let image = app.screenshot()
            let a = XCTAttachment(screenshot: image)
            a.name = "\(name) \(UITestStyle.name)"
            a.lifetime = .keepAlways
            add(a)
            if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] { // TEST_RUNNER_JARVIS_SHOTS=<dir>
                let scheme = ProcessInfo.processInfo.environment["JARVIS_STYLE"] ?? "light" // TEST_RUNNER_JARVIS_STYLE, set with simctl ui appearance
                let file = name.replacingOccurrences(of: " ", with: "-") + "-" + scheme + "-" + UITestStyle.name + ".png"
                try? image.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent(file))
            }
        }
        func text(_ s: String) -> XCUIElement {
            app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", s)).firstMatch
        }
        func inbox() throws -> [[String: Any]] {
            let data = try Data(contentsOf: URL(string: hub + "/api/v1/inbox?sources=all")!)
            return (try JSONSerialization.jsonObject(with: data) as? [String: Any])?["items"] as? [[String: Any]] ?? []
        }
        func requests() throws -> [[String: Any]] {
            let bulk = try JSONSerialization.jsonObject(with: Data(contentsOf: URL(string: hub + "/mock/inbox_bulk")!)) as? [String: Any]
            return bulk?["requests"] as? [[String: Any]] ?? []
        }
        let earlier = try requests().count
        let before = try inbox()
        let feishuOpen = before.filter { $0["account"] as? String == "feishu" && $0["done_at"] == nil }.compactMap { $0["id"] as? String }
        let othersOpen = before.filter { $0["account"] as? String != "feishu" && $0["done_at"] == nil }.compactMap { $0["id"] as? String }
        XCTAssertFalse(feishuOpen.isEmpty, "the mock has no open Feishu items")
        let firstTitle = before.first { $0["id"] as? String == feishuOpen[0] }?["title"] as? String ?? ""
        let row = text(String(firstTitle.prefix(8)))

        XCTAssertTrue(row.waitForExistence(timeout: 20), "the inbox did not load")
        let more = app.buttons["更多"].firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 5), "no 更多")
        more.tap()
        let markAll = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", entry)).firstMatch
        XCTAssertTrue(markAll.waitForExistence(timeout: 5), "no \(entry) in 更多:\n\(app.debugDescription)")
        XCTAssertTrue(text("飞书 · \(feishuOpen.count) 件").exists, "the entry does not say what it covers:\n\(app.debugDescription)")
        sleep(1)
        shot(shotName + " menu")
        markAll.tap()

        // The mock holds fewer than InboxBulk.confirmFrom Feishu items: no question first.
        XCTAssertTrue(text(toast(feishuOpen.count)).waitForExistence(timeout: 5), "no toast:\n\(app.debugDescription)")
        let undo = app.buttons["撤销"].firstMatch
        XCTAssertTrue(undo.waitForExistence(timeout: 2), "no 撤销 on the toast")
        shot(shotName + " toast")

        // One bulk request with the action, the ids and each item's version; only the filter's items filed.
        let mine = Array(try requests().dropFirst(earlier))
        XCTAssertEqual(mine.count, 1, "not one request: \(mine)")
        XCTAssertEqual(mine.last?["action"] as? String, action, "not POST /api/v1/inbox/bulk with the action: \(mine)")
        let sent = mine.last?["items"] as? [[String: Any]] ?? []
        XCTAssertEqual(Set(sent.compactMap { $0["id"] as? String }), Set(feishuOpen))
        XCTAssertTrue(sent.allSatisfy { ($0["version"] as? String)?.isEmpty == false }, "items sent without their version: \(sent)")
        let after = try inbox()
        XCTAssertTrue(after.filter { feishuOpen.contains($0["id"] as? String ?? "") }.allSatisfy { $0["done_at"] is String && $0["done_by"] as? String == doneBy }, "not all filed as \(doneBy)")
        XCTAssertTrue(after.filter { othersOpen.contains($0["id"] as? String ?? "") }.allSatisfy { $0["done_at"] == nil }, "an item outside the filter was filed")

        undo.tap()
        XCTAssertTrue(text("已移回 \(feishuOpen.count) 件").waitForExistence(timeout: 5), "no undo toast:\n\(app.debugDescription)")
        // 撤销 sends the reply's undo back: reopen, the ids this one changed.
        let undone = Array(try requests().dropFirst(earlier + 1))
        XCTAssertEqual(undone.first?["action"] as? String, "reopen", "撤销 did not send the hub's undo: \(undone)")
        XCTAssertEqual(Set(undone.first?["ids"] as? [String] ?? []), Set(feishuOpen))
        let back = try inbox()
        XCTAssertTrue(back.filter { feishuOpen.contains($0["id"] as? String ?? "") }.allSatisfy { $0["done_at"] == nil }, "撤销 did not bring them back")
        XCTAssertTrue(row.waitForExistence(timeout: 5), "the rows did not come back")
        sleep(1)
        shot(shotName + " undone")
    }

    /// 10 items and more ask first (MOCK_INBOX_EXTRA=<n> gives the mock n more 知悉即可 mails): the dialog says how many and
    /// what it covers; 取消 leaves them, the dialog's own button does them.
    @MainActor func testManyAskFirst() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        let data = try Data(contentsOf: URL(string: hub + "/api/v1/inbox")!)
        let items = (try JSONSerialization.jsonObject(with: data) as? [String: Any])?["items"] as? [[String: Any]] ?? []
        let open = items.filter { $0["done_at"] == nil }.count
        guard open >= 10 else { throw XCTSkip("needs MOCK_INBOX_EXTRA=<n> (only \(open) open)") }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "mail", "-inbox.kind", "", "-inbox.source", ""]
        app.launchPinned()
        let more = app.buttons["更多"].firstMatch
        XCTAssertTrue(more.waitForExistence(timeout: 20), "no 更多")
        sleep(3) // the list loads
        more.tap()
        let dismissAll = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "全部忽略")).firstMatch
        XCTAssertTrue(dismissAll.waitForExistence(timeout: 5), "no 全部忽略")
        dismissAll.tap()
        let ask = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "将处理")).firstMatch
        XCTAssertTrue(ask.waitForExistence(timeout: 5), "no question before \(open) items:\n\(app.debugDescription)")
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = "inbox dismiss all confirm \(UITestStyle.name)"
        shot.lifetime = .keepAlways
        add(shot)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            let scheme = ProcessInfo.processInfo.environment["JARVIS_STYLE"] ?? "light"
            try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("inbox-dismiss-all-confirm-\(scheme)-\(UITestStyle.name).png"))
        }
        app.buttons["inbox.cleanConfirm"].firstMatch.tap()
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "已忽略")).firstMatch.waitForExistence(timeout: 5),
                      "no toast after the question")
        // Bring them back: the next test needs open items.
        let undo = app.buttons["撤销"].firstMatch
        XCTAssertTrue(undo.waitForExistence(timeout: 2), "no 撤销 on the toast")
        undo.tap()
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "已移回")).firstMatch.waitForExistence(timeout: 5),
                      "撤销 did not bring them back")
    }
}
