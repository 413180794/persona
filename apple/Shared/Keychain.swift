import Foundation
import Security

nonisolated enum Keychain {
    // The phone app's bundle id on both phone and watch (the watch app's is <id>.watchkitapp).
    private static let service = (Bundle.main.bundleIdentifier ?? "jarvis").replacingOccurrences(of: ".watchkitapp", with: "")

    static func save(_ data: Data, account: String) {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                    kSecAttrAccount as String: account]
        SecItemDelete(query as CFDictionary)
        var add = query
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(add as CFDictionary, nil)
    }

    static func load(account: String) -> Data? {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                    kSecAttrAccount as String: account, kSecReturnData as String: true,
                                    kSecMatchLimit as String: kSecMatchLimitOne]
        var out: AnyObject?
        #if os(macOS) && DEBUG
        // Screenshots (`-macSnapshot`, Mac/App/MacSnapshot.swift) run while the Mac is locked, and reading an item another
        // (unsigned) build wrote then blocks in securityd for good, prompt skipped or not: they use demo data, no keychain.
        if UserDefaults.standard.string(forKey: "macSnapshot") != nil { return nil }
        // The same for the app hosting JarvisMacTests: on a locked Mac its launch otherwise never gets past this read
        // ("The test runner hung before establishing connection"); no unit test needs the keychain.
        if ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil { return nil }
        #endif
        guard SecItemCopyMatching(query as CFDictionary, &out) == errSecSuccess else { return nil }
        return out as? Data
    }

    static func delete(account: String) {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                    kSecAttrAccount as String: account]
        SecItemDelete(query as CFDictionary)
    }

    /// The watch's own, answer-only credential (DeviceCredentials.swift): what the phone hands the watch.
    static var watchConfig: ServerConfig? {
        get { load(account: "watch").flatMap { try? JSONDecoder().decode(ServerConfig.self, from: $0) } }
        set {
            if let newValue, let data = try? JSONEncoder().encode(newValue) { save(data, account: "watch") }
            else { delete(account: "watch") }
        }
    }

    static var serverConfig: ServerConfig? {
        get { load(account: "server").flatMap { try? JSONDecoder().decode(ServerConfig.self, from: $0) } }
        set {
            if let newValue, let data = try? JSONEncoder().encode(newValue) { save(data, account: "server") }
            else { delete(account: "server") }
        }
    }
}
