import XCTest

/// Opening a long session: its last exchange ran 650 commands, folded on screen into one work line
/// (「工具 650 · …」). The first page must not carry that work (it was 1400 rows and a megabyte of a real Codex session,
/// a long blank wait on a weak network); opening the work line fetches it. Against scripts/mock/mock_hub.py with MOCK_BIG=1
/// (PERSONA_MOCK_HUB), which records what each batch of the session weighed at /mock/served.
final class LongSessionTests: XCTestCase {
    private var hub: String!
    private let session = "server/a1f3c2d4-0000-4000-8000-000000000008"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_BIG=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
    }

    private struct Batch: Decodable {
        var via: String
        var items: Int
        var bytes: Int
    }

    private func served() throws -> [Batch] {
        struct Served: Decodable { var served: [Batch] }
        return try JSONDecoder().decode(Served.self, from: Data(contentsOf: URL(string: hub + "/mock/served")!)).served
    }

    @MainActor func testOpeningALongSessionLoadsLightAndUnfoldsOnDemand() throws {
        guard (try? served()) != nil else { throw XCTSkip("the mock hub has no /mock/served (start it with MOCK_BIG=1)") }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        // The default style: under BeautifulUI (left set by another test) the steps fold into chips and none shows.
        app.launchArguments = ["-pairURL", pair, "-ui.style", "glass", "-tab", "dev", "-openSession", session]
        let opened = Date.now
        app.launchPersona()

        let answer = app.staticTexts["构建脚本整理完了：650 个步骤都过了一遍。"]
        XCTAssertTrue(answer.waitForExistence(timeout: 20), "the chat did not open:\n\(app.debugDescription)")
        print("answer shown \(String(format: "%.1f", Date.now.timeIntervalSince(opened))) s after launch")

        let pages = try served().filter { $0.via != "range" }
        XCTAssertFalse(pages.isEmpty, "the app fetched nothing of the session")
        for p in pages {
            print("first page via \(p.via): \(p.items) items, \(p.bytes) bytes")
            XCTAssertLessThanOrEqual(p.items, 60, "a page of the long session carried \(p.items) items (via \(p.via))")
            XCTAssertLessThanOrEqual(p.bytes, 64 << 10, "a page of the long session weighed \(p.bytes) bytes (via \(p.via))")
        }
        // The folded work still counts everything in it.
        let work = app.buttons["work-head"].firstMatch // a layout pass can briefly hold the line twice
        XCTAssertTrue(work.waitForExistence(timeout: 5), "no work line")
        XCTAssertTrue((work.value as? String ?? "").contains("工具 650"), "the work line says \(work.value ?? "nothing")")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-long-folded.png"))

        // Opening the work line fetches the steps it folds (on a slow network: a row says they are loading).
        _ = try? Data(contentsOf: URL(string: hub + "/mock/delay?s=3")!)
        work.tap()
        let loading = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "正在载入")).firstMatch
        XCTAssertTrue(loading.waitForExistence(timeout: 3), "nothing shows the steps are loading:\n\(app.debugDescription)")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-long-loading.png"))
        _ = try? Data(contentsOf: URL(string: hub + "/mock/delay?s=0")!)
        let step = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "make step-")).firstMatch
        XCTAssertTrue(step.waitForExistence(timeout: 15), "the steps did not load:\n\(app.debugDescription)")
        XCTAssertTrue(try served().contains { $0.via == "range" }, "the steps came without a fetch")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-long-open.png"))
    }
}
