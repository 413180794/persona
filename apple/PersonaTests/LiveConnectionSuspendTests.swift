import Foundation
import Testing
@testable import Persona

/// In the background nothing opens the socket but a silent push (`LiveConnection.suspended`, `wake()`): iOS suspends
/// the app with it open, and the hub would take the phone for an open app (no wake push for its phone_* calls, no
/// Live Activity pushes) until its pings time out.
@MainActor
struct LiveConnectionSuspendTests {
    /// Nothing listens there: an attempt shows as `.connecting` and goes nowhere.
    let config = ServerConfig(url: URL(string: "http://127.0.0.1:9")!, token: "t")

    @Test func suspendedOpensNothing() {
        let live = LiveConnection()
        live.suspended = true
        live.connect(config)
        #expect(live.state == .idle)
        live.ensureConnected() // a refresh finishing
        live.reconnectNow() // the network coming back
        #expect(live.state == .idle)
    }

    @Test func aSilentPushConnectsThoughSuspended() {
        let live = LiveConnection()
        live.suspended = true
        live.connect(config)
        live.wake()
        #expect(live.state == .connecting)
        live.disconnect()
        live.suspended = true // its window over, still in the background
        live.reconnectNow()
        #expect(live.state == .idle)
    }

    @Test func backInFrontConnects() {
        let live = LiveConnection()
        live.suspended = true
        live.connect(config)
        live.suspended = false
        live.ensureConnected()
        #expect(live.state == .connecting)
        live.disconnect()
    }

    #if os(iOS)
    /// While it is connected the hub sends its Live Activity no pushes (hub/liveactivity.go): it says which one is its.
    @Test func thePhoneSaysItsPushID() {
        let m = AppModel.registration(deviceName: "iPhone", tools: [], simulator: false, deviceID: "D1")
        #expect(m["device_id"] as? String == "D1")
    }
    #endif
}
