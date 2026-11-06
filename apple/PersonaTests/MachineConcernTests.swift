import Foundation
import Testing
@testable import Jarvis

/// The order of Machines (docs/machines.md「App」): what needs a look first, otherwise the hub's order.
@MainActor struct MachineConcernTests {
    private func machine(_ id: String, online: Bool = true, mismatch: Bool? = nil) -> Machine {
        Machine(id: id, name: id, kind: "ssh", online: online, nodeMismatch: mismatch)
    }

    @Test func concerns() {
        #expect(MachineConcern.of(machine("a", online: false), alert: .critical, waiting: 3) == .offline)
        #expect(MachineConcern.of(machine("a"), alert: .critical, waiting: 3) == .critical)
        #expect(MachineConcern.of(machine("a"), alert: .warning, waiting: 3) == .warning)
        #expect(MachineConcern.of(machine("a", mismatch: true), alert: .normal, waiting: 1) == .needsYou)
        #expect(MachineConcern.of(machine("a", mismatch: true), alert: .normal, waiting: 0) == .mismatch)
        #expect(MachineConcern.of(machine("a", mismatch: false), alert: .normal, waiting: 0) == nil)
    }

    @Test func mostPressingFirstThenTheHubsOrder() {
        let concern: [String: MachineConcern] = ["calm2": .mismatch, "wait1": .needsYou, "off": .offline, "wait2": .needsYou, "hot": .warning]
        let hub = ["calm1", "wait1", "calm2", "hot", "wait2", "off", "calm3"].map { machine($0) }
        let order = MachineConcern.ordered(hub) { concern[$0.id] }.map(\.id)
        #expect(order == ["off", "hot", "wait1", "wait2", "calm2", "calm1", "calm3"])
    }

    @Test func tally() throws {
        func s(_ state: String) throws -> Session {
            let json = """
            {"machine_id":"m","session_id":"\(state)","title":"t","cwd":"/tmp/p","status":"idle","state":"\(state)"}
            """
            return try JSON.decoder.decode(Session.self, from: Data(json.utf8))
        }
        let t = AgentTally(try ["needs_you", "working", "working", "unread", "idle", "unknown"].map(s))
        #expect(t.needsYou == 1 && t.working == 2 && t.idle == 3)
    }
}
