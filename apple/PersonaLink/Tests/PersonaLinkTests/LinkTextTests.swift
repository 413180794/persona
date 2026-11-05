import Foundation
import XCTest
@testable import PersonaLink

final class LinkTextTests: XCTestCase {
    func testDuration() throws {
        let since = Date(timeIntervalSince1970: 1_000_000)
        let cases = [(30, "不到 1 分钟"), (60, "1 分钟"), (3660, "1 小时 1 分钟"), (7320, "2 小时 2 分钟")]
        for (seconds, expected) in cases {
            XCTAssertEqual(LinkText.duration(since: since, now: Date(timeIntervalSince1970: 1_000_000 + Double(seconds))), expected)
        }
    }

    func testHubLine() throws {
        XCTAssertEqual(LinkText.hub(name: "x", mesh: nil), "hub「x」")
        XCTAssertEqual(LinkText.hub(name: "x", mesh: 1), "hub「x」· 没和别的 hub 互联")
        XCTAssertTrue(LinkText.hub(name: "x", mesh: 1).contains("没和别的 hub 互联"))
        XCTAssertEqual(LinkText.hub(name: "x", mesh: 2), "hub「x」· 互联组共 2 台 hub")
        XCTAssertTrue(LinkText.hub(name: "x", mesh: 2).contains("互联组共 2 台 hub"))
    }
}
