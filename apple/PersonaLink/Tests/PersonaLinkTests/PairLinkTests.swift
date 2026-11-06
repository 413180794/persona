import Foundation
import XCTest
@testable import PersonaLink

/// `persona://link?…` opened from outside (#4198): fills in the pairing window, never pairs.
@MainActor
final class PairLinkTests: XCTestCase {
    private let full = "persona://link?url=http%3A%2F%2F192.0.2.10%3A18660&token=jp_example&machine=macbook&hub=h_example&fp=Zm9v&tls=18662"

    func testOnlyPersonaSchemeParsesIgnoringCase() throws {
        for scheme in ["persona", "PERSONA"] {
            let raw = full.replacingOccurrences(of: "persona://", with: "\(scheme)://")
            guard case .link(let l) = PairLink.parse(URL(string: raw)!) else { return XCTFail("\(scheme) not parsed") }
            XCTAssertEqual(l.raw, raw)
            XCTAssertEqual(l.machine, "macbook")
            XCTAssertEqual(l.hubHost, "192.0.2.10:18660")
            XCTAssertTrue(l.pinned)
        }
    }

    func testWithoutFingerprintOrMachine() throws {
        let raw = "persona://link?url=https%3A%2F%2Fhub.example.com%2Fprefix&token=jp_example"
        guard case .link(let l) = PairLink.parse(raw) else { return XCTFail("not parsed") }
        XCTAssertNil(l.machine)
        XCTAssertFalse(l.pinned)
        XCTAssertEqual(l.hubHost, "hub.example.com/prefix")
    }

    func testOtherHostsAreForOtherApps() throws {
        XCTAssertEqual(PairLink.parse("persona://pair?url=http%3A%2F%2F192.0.2.10%3A18660&token=jp_example"), .otherApp)
        XCTAssertEqual(PairLink.parse("persona://open?machine=a&session=b"), .otherApp)
        XCTAssertEqual(PairLink.parse("persona://connector?addr=192.0.2.20%3A18663&code=jc_x"), .otherApp)
        XCTAssertNotNil(PairLink.notice(for: .otherApp))
        XCTAssertTrue(PairLink.notice(for: .otherApp)!.contains("这是给手机 / Mac App 用的链接"))
    }

    func testMissingTokenIsIncomplete() throws {
        XCTAssertEqual(PairLink.parse("persona://link?url=http%3A%2F%2F192.0.2.10%3A18660&machine=macbook"), .incomplete)
        XCTAssertEqual(PairLink.parse("persona://link?url=http%3A%2F%2F192.0.2.10%3A18660&token="), .incomplete)
        XCTAssertNotNil(PairLink.notice(for: .incomplete))
    }

    func testNotOurs() throws {
        for other in ["other-product", "personas", "personad"] {
            XCTAssertEqual(PairLink.parse("\(other)://link?token=fixture"), .notOurs)
        }
        XCTAssertEqual(PairLink.parse("https://example.com/link?token=x"), .notOurs)
        XCTAssertEqual(PairLink.parse("not a url"), .notOurs)
        XCTAssertNil(PairLink.notice(for: .notOurs))
    }

    /// Opening the link fills the window in (the link and its machine) and does nothing else.
    func testOpeningPrefillsWithoutPairing() throws {
        let m = LinkModel()
        m.openLink(URL(string: full)!)
        XCTAssertEqual(m.incoming?.raw, full)
        XCTAssertEqual(m.incoming?.machine, "macbook")
        XCTAssertNil(m.incomingNotice)
        XCTAssertEqual(m.pairRuns, 0, "opening a link must never pair by itself")
        m.clearIncoming()
        XCTAssertNil(m.incoming)
        XCTAssertEqual(m.pairRuns, 0)
    }

    func testOpeningAnotherAppsLinkOnlySaysSo() throws {
        let m = LinkModel()
        m.openLink(URL(string: full)!)
        m.clearIncoming()
        m.openLink(URL(string: "persona://pair?url=http%3A%2F%2F192.0.2.10%3A18660&token=jp_example")!)
        XCTAssertNil(m.incoming)
        XCTAssertNotNil(m.incomingNotice)
        XCTAssertEqual(m.pairRuns, 0)
        // A link for this app afterwards takes the notice away.
        m.openLink(URL(string: full)!)
        XCTAssertNil(m.incomingNotice)
        XCTAssertNotNil(m.incoming)
    }

    /// The status lines' `pending`: shown as 「等你确认」 (waiting for you to confirm) while the hub waits for 「是我的电脑」; without it, connected.
    func testPendingLook() throws {
        let m = LinkModel()
        var s = LinkStatus(state: "connected", machine: "macbook", node: "online")
        m.show(s)
        XCTAssertEqual(m.look, .connected)
        s.pending = true
        m.show(s)
        XCTAssertEqual(m.look, .awaitingConfirm)
        s.pending = false
        s.node = "offline"
        m.show(s)
        XCTAssertEqual(m.look, .nodeMissing)
        let line = #"{"state":"connected","machine":"macbook","node":"online","pending":true}"#
        XCTAssertEqual(try LinkModel.decoder.decode(LinkStatus.self, from: Data(line.utf8)).pending, true)
        let older = #"{"state":"connected","machine":"macbook","node":"online"}"#
        XCTAssertNil(try LinkModel.decoder.decode(LinkStatus.self, from: Data(older.utf8)).pending)
    }
}
