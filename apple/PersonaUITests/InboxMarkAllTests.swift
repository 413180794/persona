import XCTest

/// 更多 › 全部标为已处理 (docs/inbox.md「全部标为已处理」): the menu says what it covers (the 飞书 filter here) and how
/// many; one request files them all, with their versions; the toast's 撤销 brings them back. Against
/// scripts/mock/mock_hub.py (JARVIS_MOCK_HUB), which keeps the bulk requests at /mock/inbox_bulk; a fresh mock per run
/// (the test counts the requests). `TEST_RUNNER_JARVIS_SHOTS=<dir>` also writes the screenshots there.
final class InboxMarkAllTests: XCTestCase {
    @MainActor func testMarkAllInTheFilterThenUndo() throws {
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
        let markAll = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "全部标为已处理")).firstMatch
        XCTAssertTrue(markAll.waitForExistence(timeout: 5), "no 全部标为已处理 in 更多:\n\(app.debugDescription)")
        XCTAssertTrue(text("飞书 · \(feishuOpen.count) 件").exists, "the entry does not say what it covers:\n\(app.debugDescription)")
        sleep(1)
        shot("inbox mark all menu")
        markAll.tap()

        XCTAssertTrue(text("已将 \(feishuOpen.count) 件标为已处理").waitForExistence(timeout: 5), "no toast:\n\(app.debugDescription)")
        let undo = app.buttons["撤销"].firstMatch
        XCTAssertTrue(undo.waitForExistence(timeout: 2), "no 撤销 on the toast")
        shot("inbox mark all toast")

        // One request, with each item's version; only the filter's items filed.
        let bulk = try JSONSerialization.jsonObject(with: Data(contentsOf: URL(string: hub + "/mock/inbox_bulk")!)) as? [String: Any]
        let requests = bulk?["requests"] as? [[String: Any]] ?? []
        XCTAssertEqual(requests.count, 1, "not one request: \(requests)")
        let sent = requests.last?["items"] as? [[String: Any]] ?? []
        XCTAssertEqual(Set(sent.compactMap { $0["id"] as? String }), Set(feishuOpen))
        XCTAssertTrue(sent.allSatisfy { ($0["version"] as? String)?.isEmpty == false }, "items sent without their version: \(sent)")
        let after = try inbox()
        XCTAssertTrue(after.filter { feishuOpen.contains($0["id"] as? String ?? "") }.allSatisfy { $0["done_at"] is String }, "not all filed")
        XCTAssertTrue(after.filter { othersOpen.contains($0["id"] as? String ?? "") }.allSatisfy { $0["done_at"] == nil }, "an item outside the filter was filed")

        undo.tap()
        XCTAssertTrue(text("已移回 \(feishuOpen.count) 件").waitForExistence(timeout: 5), "no undo toast:\n\(app.debugDescription)")
        let back = try inbox()
        XCTAssertTrue(back.filter { feishuOpen.contains($0["id"] as? String ?? "") }.allSatisfy { $0["done_at"] == nil }, "撤销 did not bring them back")
        XCTAssertTrue(row.waitForExistence(timeout: 5), "the rows did not come back")
        sleep(1)
        shot("inbox mark all undone")
    }
}
