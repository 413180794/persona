import Foundation
import Testing
@testable import Persona

/// The SSH tunnels' ends (#2379): removing a group closes its hubs' tunnels (not another group's), and a tunnel
/// adopted under the paired hub's id has its phase written there, not under the id it was started with.
@MainActor
struct SSHTunnelLifecycleTests {
    private static func hub(_ id: String, group: String) -> PairedHub {
        PairedHub(id: id, config: ServerConfig(url: URL(string: "http://\(id).example.com")!, token: "t-\(id)"), group: group)
    }

    @Test func removingAGroupClosesItsHubsTunnelsOnly() {
        var r = HubRegistry()
        r.hubs = [Self.hub("a1", group: "home"), Self.hub("b1", group: "work"), Self.hub("a2", group: "home")]
        #expect(AppModel.tunnelIDs(removing: "home", from: r) == ["a1", "a2"])
        #expect(AppModel.tunnelIDs(removing: "work", from: r) == ["b1"])
        #expect(AppModel.tunnelIDs(removing: "gone", from: r).isEmpty)
    }

    private final class Tunnel {}
    private struct Open { var tunnel: Tunnel }

    @Test func anAdoptedTunnelIsFoundUnderItsNewID() {
        let t = Tunnel(), other = Tunnel()
        var table: [String: Open] = [SSHTunnels.pairingID: Open(tunnel: t), "b1": Open(tunnel: other)]
        #expect(SSHTunnels.key(of: t, in: table, \.tunnel) == SSHTunnels.pairingID)
        // adopt: the paired hub keeps the tunnel it was paired through, under its own id
        table["a1"] = table.removeValue(forKey: SSHTunnels.pairingID)
        #expect(SSHTunnels.key(of: t, in: table, \.tunnel) == "a1")
        #expect(SSHTunnels.key(of: other, in: table, \.tunnel) == "b1")
        // closed: its late phases go nowhere
        table["a1"] = nil
        #expect(SSHTunnels.key(of: t, in: table, \.tunnel) == nil)
    }
}
