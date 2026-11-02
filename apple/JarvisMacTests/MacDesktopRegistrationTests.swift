import Testing
@testable import Jarvis

/// A Mac registers with the hub as a desktop, never as the phone: the iPhone keeps the phone_* tools and its wake
/// pushes (docs/mac.md「Mac 是桌面端，不是手机」).
struct MacDesktopRegistrationTests {
    @Test func theMacRegistersAsADesktop() {
        let m = AppModel.registration(deviceName: "MacBook", tools: [["name": "phone_device_info"]], simulator: false)
        #expect(m["type"] as? String == "register_desktop")
        #expect(m["device_name"] as? String == "MacBook")
        #expect(m["tools"] == nil)
    }

    /// Its push id goes along: while this Mac is connected the hub leaves the agents' alerts to it (MacAttentionWatcher)
    /// instead of pushing them to it as well.
    @Test func itSaysWhichPushDeviceItIs() {
        let m = AppModel.registration(deviceName: "MacBook", tools: [], simulator: false, deviceID: "D1")
        #expect(m["device_id"] as? String == "D1")
    }
}
