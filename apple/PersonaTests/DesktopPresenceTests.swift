import Foundation
import Testing
@testable import Persona

/// 「Mac 上的 Persona 已登录」(docs/protocol/protocol.md「桌面端」): the hub says which desktops are connected; a phone may
/// choose Phone Muted while one is.
struct DesktopPresenceTests {
    @Test func decodesTheDesktopsEvent() throws {
        let json = #"{"type":"desktops","desktops":[{"device":"dev_1","name":"MacBook","since":"2026-11-01T08:00:00.123456789+08:00"}]}"#
        guard case .desktops(let list)? = ServerEvent.decode(Data(json.utf8)) else { Issue.record("not decoded"); return }
        #expect(list.map(\.name) == ["MacBook"])
        #expect(list.first?.device == "dev_1")
        #expect(list.first?.since != nil)
    }

    @Test func noDesktopsIsAnEmptyList() throws {
        guard case .desktops(let list)? = ServerEvent.decode(Data(#"{"type":"desktops","desktops":[]}"#.utf8)) else {
            Issue.record("not decoded"); return
        }
        #expect(list.isEmpty)
    }

    @Test func muteWhileDesktopRoundTrips() throws {
        #expect(try JSON.decoder.decode(NotifyPrefs.self, from: Data("{}".utf8)).muteWhileDesktop == false)
        var p = try JSON.decoder.decode(NotifyPrefs.self, from: Data(#"{"mute_while_desktop":true}"#.utf8))
        #expect(p.muteWhileDesktop)
        // Settings › Notifications saves the whole NotifyPrefs it loaded: it must not carry Phone Muted back (an older value would undo a
        // switch made on the banner meanwhile). The banner writes it alone.
        let obj = try JSONSerialization.jsonObject(with: try JSON.encoder.encode(p)) as? [String: Any]
        #expect(obj?["mute_while_desktop"] == nil)
        let alone = try JSONSerialization.jsonObject(with: try JSON.encoder.encode(MuteWhileDesktopPut(on: true))) as? [String: Any]
        #expect(alone?.count == 1 && alone?["mute_while_desktop"] as? Bool == true)
    }

    /// The banner's line: one desktop by kind, several by count.
    @Test func bannerTitle() {
        let one = [DesktopDevice(device: "d1", name: "MacBook", since: nil)]
        #expect(DesktopDevice.bannerTitle(one) == "Mac 上的 Persona 已登录")
        #expect(DesktopDevice.bannerTitle(one + [DesktopDevice(device: "d2", name: "iMac", since: nil)]) == "2 台 Mac 上的 Persona 已登录")
    }

    #if os(iOS)
    /// The iPhone is the phone: it registers its tools (the Mac registers as a desktop, PersonaMacTests).
    @Test func thePhoneRegistersAsThePhone() {
        let m = AppModel.registration(deviceName: "iPhone", tools: [], simulator: false)
        #expect(m["type"] as? String == "register_phone")
        #expect(m["tools"] != nil)
    }
    #endif
}
