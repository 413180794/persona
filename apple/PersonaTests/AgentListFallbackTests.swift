import Foundation
import Testing
@testable import Persona

@MainActor struct AgentListFallbackTests {
    @Test func runtimePreferencesNeverFilterAPartialWindow() {
        #expect(!AgentListLoadingPolicy.runtime(grouping: .recent, filter: .all, query: "", hasPins: false, hasMutes: false))
        for grouping in DevGrouping.allCases where grouping != .recent {
            #expect(AgentListLoadingPolicy.runtime(grouping: grouping, filter: .all, query: "", hasPins: false, hasMutes: false))
        }
        for filter in AgentFilter.allCases where filter != .all {
            #expect(AgentListLoadingPolicy.runtime(grouping: .recent, filter: filter, query: "", hasPins: false, hasMutes: false))
        }
        #expect(AgentListLoadingPolicy.runtime(grouping: .recent, filter: .all, query: "search", hasPins: false, hasMutes: false))
        #expect(AgentListLoadingPolicy.runtime(grouping: .recent, filter: .all, query: "", hasPins: true, hasMutes: false))
        #expect(AgentListLoadingPolicy.runtime(grouping: .recent, filter: .all, query: "", hasPins: false, hasMutes: true))
    }

    @Test func personalPreferencesRetainGlobalMembershipAndOrdering() {
        let plain = PersonalAgentPrefs()
        #expect(!AgentListLoadingPolicy.personal(plain, query: ""))
        #expect(AgentListLoadingPolicy.personal(plain, query: "provider alias"))
        var variants: [PersonalAgentPrefs] = []
        var p = plain; p.pins = ["demo:old"]; variants.append(p)
        p = plain; p.addSection("Example", id: "custom"); variants.append(p)
        p = plain; p.assign = ["demo:old": "custom"]; variants.append(p)
        p = plain; p.view.sort = AgentSortBy.name.rawValue; variants.append(p)
        p = plain; p.view.sort = AgentSortBy.unread.rawValue; variants.append(p)
        p = plain; p.view.show = AgentShow.sendable.rawValue; variants.append(p)
        p = plain; p.view.show = AgentShow.unread.rawValue; variants.append(p)
        p = plain; p.view.providers = ["one", "two"]; variants.append(p)
        for p in variants { #expect(AgentListLoadingPolicy.personal(p, query: "")) }
        p = plain; p.view.providers = ["one"]
        #expect(!AgentListLoadingPolicy.personal(p, query: ""))
    }

    @Test func fullRuntimeFallbackIncludesMatchesAfterTheFirstPageAndNeverSendsNewFilters() async throws {
        let hub = FakeHub()
        let rows = (0..<8000).map { n in
            #"{"machine_id":"demo","session_id":"s\#(n)","title":"Agent \#(n)","state":"idle","pane_id":"\#(n == 7999 ? "botmux:tail" : "pane")"}"#
        }.joined(separator: ",")
        let payload = Data(#"{"sessions":[\#(rows)]}"#.utf8)
        hub.on("GET", "/api/v1/sessions") { request in
            #expect(request.query.isEmpty, "full fallback must use the old no-opt-in path")
            return payload
        }
        var countCalls = 0
        let list = SessionListModel<Session>(id: { $0.id }, removalID: { $0 },
            fetch: { try await hub.client.sessionListPage($0, cursor: $1, since: $2) },
            counts: { _ in countCalls += 1; throw APIError(status: 0, message: "no partial counts") })
        await list.select(SessionListQuery(legacy: true, state: "working", source: "not-a-host", text: "tail"))
        #expect(list.rows.count == 8000)
        #expect(!list.paged && !list.hasMore)
        #expect(countCalls == 0)
        #expect(list.rows.filter { AgentFilter.feishu.matches($0) }.map(\.sessionID) == ["s7999"])
        #expect(list.rows.contains { $0.title == "Agent 7999" })
    }

    @Test func personalFullFallbackUsesNoMessageSearchOrProviderTruncation() async throws {
        let hub = FakeHub()
        hub.on("GET", "/api/v1/agents/sessions") { request in
            #expect(request.query.isEmpty)
            return Data(#"{"sessions":[{"id":"demo:tail","source":"demo","sessionKey":"tail","provider":"two","title":"Tail","canSend":true,"unreadCount":7,"rankTimeMs":1}]}"#.utf8)
        }
        let page = try await hub.client.personalAgentListPage(SessionListQuery(legacy: true, provider: "one", text: "alias"))
        #expect(!page.isPaged)
        #expect(page.sessions.first?.provider == "two")
        #expect(page.sessions.first?.unreadCount == 7)
    }

    @Test func unknownCountsDoNotBecomeZeroOrRemoveFilterChoices() {
        let unknown = QuickFilterBar(selection: .all, counts: [:], countsUnknown: true, choose: { _ in })
        let known = QuickFilterBar(selection: .all, counts: [:], choose: { _ in })
        #expect(unknown != known)
        #expect(unknown.availableFilters == AgentFilter.allCases)
        #expect(unknown.counts.isEmpty)
    }
}
