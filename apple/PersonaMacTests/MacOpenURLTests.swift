import Foundation
import Testing
@testable import Jarvis

/// `jarvis://` links handed to the Mac app (another app, a shortcut, the browser): `jarvis://open?machine=&session=`
/// opens that chat, as the iPhone does for its Live Activity's links; `jarvis://pair?…` pairs; anything else is ignored.
struct MacOpenURLTests {
    @Test func bothProductSchemesRouteTheSameWay() {
        for scheme in ["persona", "jarvis", "PERSONA", "JARVIS"] {
            #expect(MacOpenURL.route(URL(string: "\(scheme)://open?machine=mac&session=abc-1")!)
                    == .session(SessionKey(machineID: "mac", sessionID: "abc-1")))
            #expect(MacOpenURL.route(URL(string: "\(scheme)://open")!) == .app)
            #expect(MacOpenURL.route(URL(string: "\(scheme)://pair?url=https%3A%2F%2Fhub.example.com&token=t")!) == .pair)
            #expect(MacOpenURL.route(URL(string: "\(scheme)://settings")!) == .ignore)
        }
    }
    @Test func openLinksNameAChat() {
        #expect(MacOpenURL.route(URL(string: "jarvis://open?machine=mac&session=abc-1")!) == .session(SessionKey(machineID: "mac", sessionID: "abc-1")))
    }

    /// The Live Activity's link for the whole fleet: just the app.
    @Test func openWithoutASessionIsTheApp() {
        #expect(MacOpenURL.route(URL(string: "jarvis://open")!) == .app)
        #expect(MacOpenURL.route(URL(string: "jarvis://open?machine=mac")!) == .app)
    }

    @Test func pairLinksPair() {
        #expect(MacOpenURL.route(URL(string: "jarvis://pair?url=https%3A%2F%2Fhub.example.com&token=t")!) == .pair)
    }

    @Test func otherLinksAreIgnored() {
        #expect(MacOpenURL.route(URL(string: "https://example.com/open?machine=a&session=b")!) == .ignore)
        #expect(MacOpenURL.route(URL(string: "jarvis://settings")!) == .ignore)
    }
}
