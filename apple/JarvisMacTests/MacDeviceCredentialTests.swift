import Testing
@testable import Jarvis

/// The Mac's own credential is not a phone's: 设置 › 设备 shows it as a computer, and only the iPhone is the phone
/// (docs/mac.md「Mac 是桌面端，不是手机」). The hub keeps kinds other than phone / watch as "other", with full rights.
struct MacDeviceCredentialTests {
    @Test func theMacsCredentialIsNotAPhone() {
        #expect(AppModel.credentialKind == "desktop")
    }
}
