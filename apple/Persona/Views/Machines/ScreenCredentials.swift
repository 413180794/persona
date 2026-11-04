import Security
import SwiftUI

/// A Mac's Screen Sharing account and password remembered on this device (#1322, 「在这台手机上记住」), by machine: in
/// the keychain under a service of their own, readable only while the device is unlocked and never leaving it (not in
/// the iCloud keychain, not restored onto another device). From here they go only into the live screen's page
/// (ScreenBridge) when the Mac asks to sign in, never to the hub or a log. One-tap unlock (docs/computer-use.md
/// 「锁屏、防睡眠、按 App 批准」a-2) reads the same.
nonisolated enum ScreenCredentials {
    struct Saved: Codable, Equatable, Sendable {
        var account: String
        var password: String
    }

    static let service = (Bundle.main.bundleIdentifier ?? "persona").replacingOccurrences(of: ".watchkitapp", with: "") + ".screen-sharing"

    static func key(_ ref: MachineRef) -> String { ref.group.isEmpty ? ref.id : ref.group + "/" + ref.id }

    /// This service's items, or ref's one.
    private static func query(_ ref: MachineRef?) -> [String: Any] {
        var q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service]
        if let ref { q[kSecAttrAccount as String] = key(ref) }
        return q
    }

    static func load(_ ref: MachineRef) -> Saved? {
        #if os(macOS) && DEBUG
        // As Keychain.load: the app hosting the Mac's unit tests never touches the keychain.
        if ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil { return nil }
        #endif
        var q = query(ref)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else { return nil }
        return try? JSONDecoder().decode(Saved.self, from: data)
    }

    @discardableResult
    static func save(_ saved: Saved, for ref: MachineRef) -> Bool {
        guard !saved.account.isEmpty, !saved.password.isEmpty, let data = try? JSONEncoder().encode(saved) else { return false }
        SecItemDelete(query(ref) as CFDictionary)
        var add = query(ref)
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        add[kSecAttrSynchronizable as String] = false
        return SecItemAdd(add as CFDictionary, nil) == errSecSuccess
    }

    static func forget(_ ref: MachineRef) { SecItemDelete(query(ref) as CFDictionary) }

    /// Every Mac's (`-openScreenFresh YES`: the UI tests start with none).
    static func forgetAll() { SecItemDelete(query(nil) as CFDictionary) }
}

/// 忘记屏幕共享密码, next to 看画面 while this device remembers the Mac's sign-in: the live screen asks for it again.
struct ScreenForgetButton: View {
    let ref: MachineRef
    @Binding var kept: Bool

    var body: some View {
        Button {
            ScreenCredentials.forget(ref)
            kept = false
            Haptics.success()
        } label: {
            Text("忘记屏幕共享密码").font(Theme.Font.labelStrong).lineLimit(1).minimumScaleFactor(0.8)
        }
        .themedButton()
        .accessibilityIdentifier("screen-forget")
    }
}
