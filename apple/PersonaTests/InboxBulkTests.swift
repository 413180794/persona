import XCTest
@testable import Jarvis

/// 全部标为已处理's arithmetic (InboxBulk): what a request moves, how the list shows it before and after the hub's
/// answer, the toast, and the scope line in the menu.
final class InboxBulkTests: XCTestCase {
    private func item(_ id: String, done: Bool = false, version: String = "v1") throws -> InboxItem {
        let json = """
        {"id":"\(id)","source":"lark","kind":"dm","account":"feishu","account_name":"飞书","title":"\(id)","preview":"","count":1,
         "version":"\(version)"\(done ? ",\"done_at\":\"2026-11-04T08:00:00Z\",\"done_by\":\"read\"" : "")}
        """
        return try JSON.decoder.decode(InboxItem.self, from: Data(json.utf8))
    }

    func testTargetsAreTheOnesNotInThatStateOnce() throws {
        let a = try item("a"), b = try item("b", done: true), c = try item("c")
        XCTAssertEqual(InboxBulk.targets([a, b, c, a], done: true).map(\.id), ["a", "c"])
        XCTAssertEqual(InboxBulk.targets([a, b, c], done: false).map(\.id), ["b"])
        XCTAssertEqual(try JSON.decoder.decode(InboxItem.self, from: JSONEncoder().encode(a)).version, "v1", "the version is kept in the cache")
    }

    func testMarkingThenSettling() throws {
        let a = try item("a"), b = try item("b"), c = try item("c"), d = try item("d")
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let shown = InboxBulk.marking([a, b, c, d], ids: ["a", "b", "c"], done: true, now: now)
        XCTAssertEqual(shown.filter(\.isDone).map(\.id), ["a", "b", "c"])
        XCTAssertEqual(shown.first?.doneBy, "user")
        XCTAssertFalse(shown[3].isDone, "an item outside the scope was filed")

        // The hub filed a, b took a new message meanwhile, c is gone.
        var filed = a
        filed.doneAt = now
        filed.doneBy = "user"
        filed.version = "v2"
        let result = InboxBulkResult(changed: [filed], unchanged: [], skipped: [.init(id: "b", reason: "changed"), .init(id: "c", reason: "missing")])
        let settled = InboxBulk.settle(shown, before: ["a": a, "b": b, "c": c], result: result)
        XCTAssertEqual(settled.map(\.id), ["a", "b", "c", "d"])
        XCTAssertEqual(settled[0].version, "v2", "the hub's copy replaces what was shown")
        XCTAssertFalse(settled[1].isDone, "an item with new messages stays open")
        XCTAssertFalse(settled[2].isDone, "an item the hub did not have goes back as it was")
        XCTAssertEqual(InboxBulk.changedSince(result), 1)
        XCTAssertEqual(InboxBulk.message(done: true, result: result), "已将 1 件标为已处理，1 件有新消息，留在收件箱")
    }

    func testMessages() {
        let one = InboxBulkResult(changed: [], unchanged: ["a"], skipped: [])
        XCTAssertEqual(InboxBulk.message(done: true, result: one), "没有要标的")
        XCTAssertEqual(InboxBulk.message(done: false, result: one), "没有要移回的")
    }

    func testScopeLine() {
        XCTAssertEqual(InboxBulk.scopeLine(filters: [], query: "", count: 12), "全部 · 12 件")
        XCTAssertEqual(InboxBulk.scopeLine(filters: ["飞书", "@我"], query: "", count: 3), "飞书 · @我 · 3 件")
        XCTAssertEqual(InboxBulk.scopeLine(filters: [], query: " 周报 ", count: 2), "搜索「周报」 · 2 件")
        XCTAssertEqual(InboxBulk.scopeLine(filters: ["飞书"], query: "", count: 0), "没有未处理的")
    }
}
