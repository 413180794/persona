import Foundation
import Testing
@testable import Persona

/// What the phone gives the watch (#1185): each group's answer-only watch credential, never the phone's own in its
/// place (that one opens everything), and which groups have none yet so the watch can ask for a sync.
@MainActor struct WatchCredentialTests {
    private static func cfg(_ host: String, _ token: String) -> ServerConfig {
        ServerConfig(url: URL(string: "http://\(host).example.com")!, token: token)
    }

    private static let registry = HubRegistry(hubs: [
        PairedHub(id: "a", config: cfg("a", "phone-a"), watch: cfg("a", "watch-a")),
        PairedHub(id: "b", config: cfg("b", "phone-b"), group: "g2"), // the hub did not give a watch token
        PairedHub(id: "c", config: cfg("c", "phone-c"), group: "g2", watch: cfg("c", "watch-c")), // not the group's first
    ])

    @Test func onlyTheWatchsOwnCredentialGoesToTheWatch() {
        let (configs, missing) = Self.registry.watchCredentials()
        #expect(configs == ["": Self.cfg("a", "watch-a")])
        #expect(missing == ["g2"])
        let phone = Set(Self.registry.hubs.map(\.config.token))
        #expect(configs.values.allSatisfy { !phone.contains($0.token) })
    }

    @Test func aPhoneWithoutAnyWatchCredentialGivesTheWatchNone() {
        let r = HubRegistry(hubs: [PairedHub(id: "a", config: Self.cfg("a", "phone-a"))])
        let (configs, missing) = r.watchCredentials()
        #expect(configs.isEmpty)
        #expect(missing == [""])
    }

    @Test func theModelsWatchConfigsSayWhatIsMissing() {
        let model = AppModel()
        model.registry = Self.registry
        let all = model.watchConfigs()
        #expect(all.configs == ["": Self.cfg("a", "watch-a")])
        #expect(all.missing == ["g2"])
        #expect(Set(all.names.keys) == ["", "g2"])
    }

    /// An older phone's message has no `missing`; a newer one's still reads on an older watch (unknown keys are skipped).
    @Test func theMessageReadsBothWays() throws {
        let old = try JSONDecoder().decode(WatchConfigs.self, from: Data(#"{"configs":{},"names":{}}"#.utf8))
        #expect(old.missing == nil)
        let all = WatchConfigs(configs: [:], names: ["": "hub"], missing: [""])
        #expect(try JSONDecoder().decode(WatchConfigs.self, from: JSONEncoder().encode(all)) == all)
    }
}
