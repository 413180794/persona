import CryptoKit
import Foundation
import Security
import UserNotifications

/// Pushes sealed to this phone (docs/push-relay.md): a hub without the app's APNs key seals each payload to the phone's
/// public key and the relay forwards it as `e`. HPKE base mode, DHKEM(P-256, HKDF-SHA256) + HKDF-SHA256 + AES-256-GCM,
/// the same suite as the hub's `pushcrypto` package. The notification service extension (PersonaNotify) opens alerts,
/// the app opens silent pushes; anything that does not open stays 「有新消息」, never anything in the clear.
nonisolated enum PushCrypto {
    static let info = Data("persona push v1".utf8)
    static let suite = HPKE.Ciphersuite.P256_SHA256_AES_GCM_256
    /// A payload sealed longer ago than this shows only the fallback (a relay replaying old ones learns nothing new).
    static let maxAge: TimeInterval = 24 * 3600
    private static let encapsulatedKeyLength = 65

    // MARK: The key

    /// The keychain group the app and its extension share (Info.plist, `<team>.<bundle id>.push`); nil without a team
    /// (an empty one leaves just the dot).
    static var accessGroup: String? {
        (Bundle.main.object(forInfoDictionaryKey: "PersonaPushKeychainGroup") as? String).flatMap { $0.isEmpty || $0.hasPrefix(".") ? nil : $0 }
    }
    private static let service = "persona.push"
    private static let account = "push-key"

    /// Each group of hubs seals to a key of its own (docs/multi-hub.md「推送」): the default group's is the one the
    /// phone always had, another group's has the group's id after it. The key that opens a push says whose it is.
    static func account(group: String) -> String { group.isEmpty ? account : account + "." + group }

    private static func query(group: String?, hubGroup: String = "") -> [String: Any] {
        var q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecAttrAccount as String: account(group: hubGroup)]
        if let group { q[kSecAttrAccessGroup as String] = group }
        #if os(macOS)
        // A Mac's default is the old file keychain, where access groups do nothing and an item belongs to the app that
        // made it: PersonaMacNotify could not read the key. The data protection keychain shares it like on a phone.
        q[kSecUseDataProtectionKeychain as String] = true
        #endif
        return q
    }

    /// The private key, if this phone made one. Readable once the phone was unlocked after boot (the extension runs locked).
    static func loadKey(group hubGroup: String = "") -> P256.KeyAgreement.PrivateKey? {
        var q = query(group: accessGroup, hubGroup: hubGroup)
        q[kSecReturnData as String] = true
        q[kSecMatchLimit as String] = kSecMatchLimitOne
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let data = out as? Data else { return nil }
        return try? P256.KeyAgreement.PrivateKey(rawRepresentation: data)
    }

    /// Every group's key: (group, key), the default group's first.
    static func allKeys() -> [(group: String, key: P256.KeyAgreement.PrivateKey)] {
        var q: [String: Any] = [kSecClass as String: kSecClassGenericPassword, kSecAttrService as String: service,
                                kSecReturnData as String: true, kSecReturnAttributes as String: true,
                                kSecMatchLimit as String: kSecMatchLimitAll]
        if let accessGroup { q[kSecAttrAccessGroup as String] = accessGroup }
        var out: AnyObject?
        guard SecItemCopyMatching(q as CFDictionary, &out) == errSecSuccess, let items = out as? [[String: Any]] else {
            return loadKey().map { [("", $0)] } ?? []
        }
        var keys: [(group: String, key: P256.KeyAgreement.PrivateKey)] = []
        for item in items {
            guard let name = item[kSecAttrAccount as String] as? String, let data = item[kSecValueData as String] as? Data,
                  let key = try? P256.KeyAgreement.PrivateKey(rawRepresentation: data) else { continue }
            if name == account { keys.insert(("", key), at: 0) }
            else if name.hasPrefix(account + ".") { keys.append((String(name.dropFirst(account.count + 1)), key)) }
        }
        return keys
    }

    /// A group that is gone: its key goes too.
    static func removeKey(group hubGroup: String) {
        SecItemDelete(query(group: accessGroup, hubGroup: hubGroup) as CFDictionary)
        SecItemDelete(query(group: nil, hubGroup: hubGroup) as CFDictionary)
    }

    /// The key, made and saved the first time. It never leaves this phone: not synced, not in backups.
    static func ensureKey(group hubGroup: String = "") -> P256.KeyAgreement.PrivateKey? {
        if let key = loadKey(group: hubGroup) { return key }
        let key = P256.KeyAgreement.PrivateKey()
        var add = query(group: accessGroup, hubGroup: hubGroup)
        add[kSecValueData as String] = key.rawRepresentation
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        var status = SecItemAdd(add as CFDictionary, nil)
        if status == errSecMissingEntitlement, accessGroup != nil {
            // A build signed without the shared group (a free personal team): the app can still open silent pushes;
            // the extension cannot read it and shows the fallback.
            add.removeValue(forKey: kSecAttrAccessGroup as String)
            status = SecItemAdd(add as CFDictionary, nil)
        }
        // Only a key that reads back: one that was not saved would make the hub seal to a key this phone does not keep.
        return status == errSecSuccess || status == errSecDuplicateItem ? loadKey(group: hubGroup) : nil
    }

    /// What the hub gets: the X9.63 uncompressed point (65 bytes), base64.
    static func publicKey(_ key: P256.KeyAgreement.PrivateKey) -> String {
        key.publicKey.x963Representation.base64EncodedString()
    }

    // MARK: Opening

    static func open(_ sealed: Data, with key: P256.KeyAgreement.PrivateKey) throws -> Data {
        guard sealed.count > encapsulatedKeyLength else { throw CryptoKitError.incorrectParameterSize }
        var recipient = try HPKE.Recipient(privateKey: key, ciphersuite: suite, info: info,
                                           encapsulatedKey: sealed.prefix(encapsulatedKeyLength))
        return try recipient.open(sealed.dropFirst(encapsulatedKeyLength))
    }

    /// Whether a push came through the relay (has a sealed payload).
    static func isSealed(_ userInfo: [AnyHashable: Any]) -> Bool { userInfo["e"] is String }

    /// The payload the hub sealed into this push, or nil when there is none, it does not open, or it is too old.
    static func payload(_ userInfo: [AnyHashable: Any], key: P256.KeyAgreement.PrivateKey?, now: Date = .now) -> [String: Any]? {
        guard let key, let b64 = userInfo["e"] as? String, let sealed = Data(base64Encoded: b64),
              let plain = try? open(sealed, with: key),
              let obj = try? JSONSerialization.jsonObject(with: plain) as? [String: Any],
              let iat = (obj["iat"] as? NSNumber)?.doubleValue,
              abs(now.timeIntervalSince1970 - iat) <= maxAge else { return nil }
        return obj
    }

    /// Puts an opened payload on the notification: the texts, the buttons (category), the thread, how loud, and the ids the
    /// app reads when it is tapped. Only what the payload has replaces the fallback.
    static func apply(_ payload: [String: Any], to content: UNMutableNotificationContent) {
        let aps = payload["aps"] as? [String: Any] ?? [:]
        if let alert = aps["alert"] as? [String: Any] {
            if let v = alert["title"] as? String { content.title = v }
            if let v = alert["subtitle"] as? String { content.subtitle = v }
            if let v = alert["body"] as? String { content.body = v }
        } else if let alert = aps["alert"] as? String {
            content.body = alert
        }
        if let v = aps["category"] as? String { content.categoryIdentifier = v }
        if let v = aps["thread-id"] as? String { content.threadIdentifier = v }
        switch aps["interruption-level"] as? String {
        case "time-sensitive": content.interruptionLevel = .timeSensitive
        case "passive": content.interruptionLevel = .passive
        case "active": content.interruptionLevel = .active
        default: break
        }
        var info = content.userInfo
        info.removeValue(forKey: "e")
        for (k, v) in payload where k != "aps" && k != "iat" { info[k] = v }
        content.userInfo = info
    }

    // MARK: The extension's work

    enum Outcome: String { case notSealed, noKey, unopened, opened }

    /// What the notification service extension shows for a delivered push (PersonaNotify/NotificationService.swift):
    /// the decrypted content, or the push as it came (the relay's 「有新消息」).
    static func service(_ content: UNNotificationContent, key: () -> P256.KeyAgreement.PrivateKey? = { loadKey() },
                        now: Date = .now) -> (UNNotificationContent, Outcome) {
        service(content, keys: { key().map { [("", $0)] } ?? [] }, now: now)
    }

    /// Tries every group's key; the one that opens the push names its group (`persona_group`, docs/multi-hub.md).
    static func service(_ content: UNNotificationContent, keys: () -> [(group: String, key: P256.KeyAgreement.PrivateKey)],
                        now: Date = .now) -> (UNNotificationContent, Outcome) {
        guard isSealed(content.userInfo) else { return (content, .notSealed) }
        let all = keys()
        guard !all.isEmpty else { return (content, .noKey) }
        for (group, key) in all {
            guard let payload = payload(content.userInfo, key: key, now: now),
                  let out = content.mutableCopy() as? UNMutableNotificationContent else { continue }
            apply(payload, to: out)
            if !group.isEmpty {
                var info = out.userInfo
                info[groupKey] = group
                out.userInfo = info
            }
            return (out, .opened)
        }
        return (content, .unopened)
    }

    /// Where an opened push says which group's key opened it (another group than the default one).
    static let groupKey = "persona_group"

    /// A push's payload opened with any group's key, and the group.
    static func opened(_ userInfo: [AnyHashable: Any], keys: [(group: String, key: P256.KeyAgreement.PrivateKey)]) -> (group: String, payload: [String: Any])? {
        for (group, key) in keys { if let p = payload(userInfo, key: key) { return (group, p) } }
        return nil
    }

}
