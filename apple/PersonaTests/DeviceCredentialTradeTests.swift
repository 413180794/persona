import Foundation
import Testing
@testable import Persona

/// After pairing, the device trades the pairing token for its own (docs/protocol/protocol.md「临时配对」), and the hub drops the
/// pairing token a couple of minutes later. Every socket on it has to move to the device token, whatever address or route
/// it took, or the app sits on 401 and Connecting… for good (#2766).
@MainActor
struct DeviceCredentialTradeTests {
    /// Nothing listens on these: attempts fail and retry, nothing reaches a network.
    let paired = ServerConfig(url: URL(string: "http://127.0.0.1:9")!, token: "jp_pairing")
    let candidate = ServerConfig(url: URL(string: "http://[::1]:9")!, token: "jp_pairing")

    final class Box<T> { var value: T; init(_ v: T) { value = v } }

    private func waitUntil(_ what: () -> Bool) async {
        let deadline = Date.now + 5
        while !what(), Date.now < deadline { try? await Task.sleep(for: .milliseconds(20)) }
    }

    @Test func aSocketOnAnotherAddressTakesTheNewToken() {
        let live = LiveConnection()
        live.suspended = true // no attempt: only the config matters here
        live.connect(candidate) // a candidate address, not the one paired
        #expect(live.renewCredential(from: "jp_pairing", to: "jd_device"))
        #expect(live.currentConfig == ServerConfig(url: candidate.url, token: "jd_device"))
        #expect(!live.renewCredential(from: "jp_pairing", to: "jd_device"), "already moved")
    }

    @Test func aSocketOnAnotherHubKeepsItsToken() {
        let live = LiveConnection()
        live.suspended = true
        let other = ServerConfig(url: URL(string: "http://127.0.0.1:10")!, token: "jd_other")
        live.connect(other)
        #expect(!live.renewCredential(from: "jp_pairing", to: "jd_device"))
        #expect(live.currentConfig == other)
    }

    /// The route picks a candidate whose config still carries the pairing token (read before the trade): the attempt
    /// goes there with the device token anyway.
    @Test func aRoutedSocketKeepsTheNewTokenOnLaterAttempts() async {
        let live = LiveConnection()
        let traded = Box<[String: String]>([:])
        let routed = Box(0)
        let candidate = candidate
        live.route = { routed.value += 1; return candidate }
        live.credential = { DeviceCredentialTrade.current($0, traded: traded.value) }
        live.connect(paired)
        await waitUntil { live.currentConfig?.url == candidate.url }
        #expect(live.currentConfig == candidate)

        traded.value["jp_pairing"] = "jd_device"
        let asked = routed.value
        #expect(live.renewCredential(from: "jp_pairing", to: "jd_device"))
        await waitUntil { routed.value > asked }
        try? await Task.sleep(for: .milliseconds(50)) // the attempt after the route answered
        #expect(live.currentConfig == ServerConfig(url: candidate.url, token: "jd_device"))
        live.disconnect()
    }

    @Test func theRegistryTakesTheTokenOnlyWhileTheHubIsOnTheOldOne() {
        var r = HubRegistry()
        var hub = PairedHub(config: paired)
        hub.group = "g2" // regrouped while the trade was out: the hub is found by its id, not its group
        r.hubs = [hub]
        let out = DeviceCredentialTrade.registry(r, hub: hub.id, old: "jp_pairing", token: "jd_device", deviceID: "dev_1")
        #expect(out?.hubs.first?.config == ServerConfig(url: paired.url, token: "jd_device"))
        #expect(out?.hubs.first?.deviceID == "dev_1")
        #expect(out?.hubs.first?.tradedPairing == DeviceCredentialTrade.fingerprint("jp_pairing"))
        #expect(out?.hubs.first?.tradedPairing?.contains("jp_") == false, "only a hash is kept")

        // Paired again with another link meanwhile: that pairing trades its own.
        r.hubs[0].config.token = "jp_newer"
        #expect(DeviceCredentialTrade.registry(r, hub: hub.id, old: "jp_pairing", token: "jd_device", deviceID: "dev_1") == nil)
    }

    @Test func theSameLinkAgainKeepsTheDeviceToken() {
        var hub = PairedHub(config: ServerConfig(url: paired.url, token: "jd_device"))
        hub.deviceID = "dev_1"
        let traded = ["jp_pairing": "jd_device"]
        #expect(DeviceCredentialTrade.keepsDevice(hub, pairing: paired, traded: traded))
        #expect(!DeviceCredentialTrade.keepsDevice(hub, pairing: ServerConfig(url: paired.url, token: "jp_fresh"), traded: traded))
        #expect(DeviceCredentialTrade.current(candidate, traded: traded) == ServerConfig(url: candidate.url, token: "jd_device"))

        // Relaunched since the trade (nothing in memory): the registry's fingerprint says so.
        hub.tradedPairing = DeviceCredentialTrade.fingerprint("jp_pairing")
        #expect(DeviceCredentialTrade.keepsDevice(hub, pairing: paired, traded: [:]))
        #expect(!DeviceCredentialTrade.keepsDevice(hub, pairing: ServerConfig(url: paired.url, token: "jp_fresh"), traded: [:]))
    }

    @Test func everyGroupRefusedSaysPairingIsGone() {
        #expect(ConnectionStatus.refined(.connecting, rejected: [true]) == .rejected)
        #expect(ConnectionStatus.refined(.connecting, rejected: [true, false]) == .connecting)
        #expect(ConnectionStatus.refined(.connecting, rejected: []) == .connecting)
        #expect(ConnectionStatus.refined(.waitingForNetwork, rejected: [true]) == .waitingForNetwork)
        #expect(ConnectionStatus.refined(.online, rejected: [true]) == .online)
    }
}
