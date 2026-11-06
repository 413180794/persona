import Foundation
import Testing
@testable import Persona

@MainActor
struct PortPresenceTests {
    private let machine = MachineRef(id: "mac")
    private let path = "/api/v1/machines/mac/ports/5173"

    @Test func failedQuestionIsNotAskedAgainUntilReconnect() async throws {
        let hub = FakeHub() // no ports route: every question fails
        let presence = PortPresence()
        presence.ask([5173], machine: machine, listed: [], api: hub.client)
        try await Task.sleep(for: .milliseconds(300))
        #expect(hub.sent("GET", path).count == 1)
        #expect(presence.state(5173, machine: machine, listed: []) == nil)

        // The badge scrolls back into view: the failure is remembered, nothing is sent.
        presence.ask([5173], machine: machine, listed: [], api: hub.client)
        try await Task.sleep(for: .milliseconds(300))
        #expect(hub.sent("GET", path).count == 1)

        // Another group's reconnect changes nothing; this group's asks again.
        presence.reconnected(group: "other")
        presence.ask([5173], machine: machine, listed: [], api: hub.client)
        try await Task.sleep(for: .milliseconds(300))
        #expect(hub.sent("GET", path).count == 1)
        presence.reconnected(group: machine.group)
        presence.ask([5173], machine: machine, listed: [], api: hub.client)
        try await Task.sleep(for: .milliseconds(300))
        #expect(hub.sent("GET", path).count == 2)
    }
}
