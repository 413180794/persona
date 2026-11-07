import Foundation
import Testing
@testable import Persona

/// The hub's own links opening their page in the app (#2696, HubPageLink): which links are the hub's, and which page.
struct HubPageLinkTests {
    static let hub = URL(string: "http://hub.example.com:18660")!
    static let hubs = [hub, URL(string: "https://[2001:db8::1]:18443")!]

    func page(_ s: String, hubs: [URL] = Self.hubs) -> HubPage? { HubPageLink.page(URL(string: s)!, hubs: hubs) }

    @Test func agentSessions() {
        #expect(page("http://hub.example.com:18660/s/box1/06f51a1a-88cf-41e6-8c84-13de3620fd4e")
            == .agent(machine: "box1", session: "06f51a1a-88cf-41e6-8c84-13de3620fd4e", item: nil))
        // agent_read's link to one message (#2531): the item id is query-escaped (Claude ids hold '#').
        #expect(page("http://hub.example.com:18660/s/box1/abc?at=msg_1%23tool")
            == .agent(machine: "box1", session: "abc", item: "msg_1#tool"))
        #expect(page("http://hub.example.com:18660/s/box1/abc?view=term&at=x") == .agent(machine: "box1", session: "abc", item: "x"))
        #expect(page("http://hub.example.com:18660/s/box1/abc/") == .agent(machine: "box1", session: "abc", item: nil))
        #expect(page("http://hub.example.com:18660/s/box1/abc?at=") == .agent(machine: "box1", session: "abc", item: nil))
        // A fragment is the web page's own, not a message.
        #expect(page("http://hub.example.com:18660/s/box1/abc#bottom") == .agent(machine: "box1", session: "abc", item: nil))
        // Escaped path segments come back as written.
        #expect(page("http://hub.example.com:18660/s/my%20box/abc") == .agent(machine: "my box", session: "abc", item: nil))
        // A herdr tab with its session; without one it is a place, not a chat.
        #expect(page("http://hub.example.com:18660/t/box1/w1/t2/abc") == .agent(machine: "box1", session: "abc", item: nil))
        #expect(page("http://hub.example.com:18660/t/box1/w1/t2") == nil)
        #expect(page("http://hub.example.com:18660/s/box1") == nil)
        #expect(page("http://hub.example.com:18660/s/box1/abc/more") == nil)
    }

    @Test func conversations() {
        #expect(page("http://hub.example.com:18660/persona/c-123") == .conversation(id: "c-123", item: nil))
        #expect(page("http://hub.example.com:18660/persona/c-123?at=u1") == .conversation(id: "c-123", item: "u1"))
        #expect(page("http://hub.example.com:18660/persona/new") == nil) // the new-conversation page
        #expect(page("http://hub.example.com:18660/persona") == nil)
        #expect(page("http://hub.example.com:18660/" + LegacyNames.conversationPath + "/c-123") == .conversation(id: "c-123", item: nil))
    }

    @Test func todos() {
        #expect(page("http://hub.example.com:18660/todos?id=todo_abc") == .todo(id: "todo_abc"))
        #expect(page("http://hub.example.com:18660/todos/todo_abc") == .todo(id: "todo_abc"))
        #expect(page("http://hub.example.com:18660/todos") == nil) // the board: the web page, as before
        #expect(page("http://hub.example.com:18660/todos?id=") == nil)
    }

    @Test func unknownPathsStayWebPages() {
        for path in ["/", "/inbox/x", "/machines/box1", "/settings", "/admin", "/api/v1/sessions", "/S/box1/abc"] {
            #expect(page("http://hub.example.com:18660" + path) == nil, "\(path)")
        }
    }

    @Test func onlyThisHubsAddresses() {
        #expect(page("http://other.example.com:18660/s/box1/abc") == nil)
        #expect(page("http://hub.example.com:18661/s/box1/abc") == nil) // another port, another service
        #expect(page("http://hub.example.com/s/box1/abc") == nil) // port 80 is not 18660
        #expect(page("http://evil.example.com/?u=http://hub.example.com:18660/s/box1/abc") == nil)
        #expect(page("http://hub.example.com.evil.example:18660/s/box1/abc") == nil)
        #expect(page("persona://hub.example.com:18660/s/box1/abc") == nil)
        #expect(page("http://hub.example.com:18660/s/box1/abc", hubs: []) == nil)
        // Host case, IPv6 brackets, the scheme of the hub's TLS port.
        #expect(page("http://HUB.Example.com:18660/s/box1/abc") != nil)
        #expect(page("https://[2001:DB8::1]:18443/todos?id=t") == .todo(id: "t"))
        #expect(page("http://hub.example.com:18660/s/box1/abc", hubs: [URL(string: "https://hub.example.com:18660")!]) != nil)
        // Default ports written or not.
        #expect(page("https://hub.example.com/persona/c", hubs: [URL(string: "https://hub.example.com:443")!]) == .conversation(id: "c", item: nil))
    }

    @Test func hubUnderASubPath() {
        let hubs = [URL(string: "https://example.com/persona")!]
        #expect(page("https://example.com/persona/s/box1/abc", hubs: hubs) == .agent(machine: "box1", session: "abc", item: nil))
        #expect(page("https://example.com/persona/todos?id=t", hubs: hubs) == .todo(id: "t"))
        #expect(page("https://example.com/s/box1/abc", hubs: hubs) == nil) // outside the hub's prefix
    }

    @Test func helloCarriesPublicURL() {
        guard case .hello(let info)? = ServerEvent.decode(Data(#"{"type":"hello","features":[],"public_url":"http://hub.example.com:18660"}"#.utf8)) else {
            Issue.record("hello"); return
        }
        #expect(info.publicURL == Self.hub)
        guard case .hello(let old)? = ServerEvent.decode(Data(#"{"type":"hello","features":[]}"#.utf8)) else {
            Issue.record("old hello"); return
        }
        #expect(old.publicURL == nil)
    }
}
