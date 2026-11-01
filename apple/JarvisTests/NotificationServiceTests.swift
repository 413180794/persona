import CryptoKit
import Foundation
import Testing
import UserNotifications
@testable import Jarvis

/// The notification service extension's work (PushCrypto.service, what JarvisNotify/NotificationService.swift calls) run
/// in the app's process with the push key in the shared keychain group: `simctl push` does not run service extensions
/// (CoreSimulatorBridge adds the request like a local notification), so this is where the simulator can check it.
@MainActor
struct NotificationServiceTests {
    /// The envelope the relay writes (server/internal/relay envelope) around a payload sealed to this phone's key.
    static func relayed(_ payload: [String: Any], to key: P256.KeyAgreement.PublicKey) throws -> [AnyHashable: Any] {
        var sender = try HPKE.Sender(recipientKey: key, ciphersuite: PushCrypto.suite, info: PushCrypto.info)
        let sealed = try sender.encapsulatedKey + sender.seal(JSONSerialization.data(withJSONObject: payload))
        return ["aps": ["alert": ["title": "Jarvis", "body": "有新消息"], "mutable-content": 1, "sound": "default"],
                "e": sealed.base64EncodedString()]
    }

    static func deliver(_ userInfo: [AnyHashable: Any]) async -> UNNotificationContent {
        let content = UNMutableNotificationContent()
        content.title = "Jarvis"
        content.body = "有新消息"
        content.userInfo = userInfo
        return PushCrypto.service(content).0
    }

    @Test func opensWithTheKeyInTheSharedGroup() async throws {
        let key = try #require(PushCrypto.ensureKey())
        let payload: [String: Any] = [
            "aps": ["alert": ["title": "需要你授权", "subtitle": "重构 · 笔记本", "body": "Bash: swift test"],
                    "category": "INTERACTION_PERMISSION", "thread-id": "s1", "interruption-level": "time-sensitive"],
            "interaction_id": "ix1", "machine_id": "m1", "session_id": "s1", "iat": Date.now.timeIntervalSince1970,
        ]
        let shown = await Self.deliver(try Self.relayed(payload, to: key.publicKey))
        #expect(shown.title == "需要你授权")
        #expect(shown.body == "Bash: swift test")
        #expect(shown.categoryIdentifier == "INTERACTION_PERMISSION")
        #expect(shown.userInfo["interaction_id"] as? String == "ix1")
    }

    @Test func otherKeyStaysTheFallback() async throws {
        _ = PushCrypto.ensureKey()
        let shown = await Self.deliver(try Self.relayed(["aps": ["alert": ["body": "secret"]], "iat": Date.now.timeIntervalSince1970],
                                                        to: P256.KeyAgreement.PrivateKey().publicKey))
        #expect(shown.body == "有新消息")
    }

    @Test func notSealedIsLeftAlone() async {
        let shown = await Self.deliver(["aps": ["alert": ["body": "direct"]]])
        #expect(shown.body == "有新消息") // untouched (a direct push without mutable-content never gets here anyway)
    }

    /// scripts/relay_sim_e2e.sh: the very bytes the local relay handed to (fake) APNs, sealed by the hub's Go code to
    /// the key this app exported. RELAY_PAYLOADS: "<file>=<text it must show>|…".
    @Test(.enabled(if: ProcessInfo.processInfo.environment["RELAY_PAYLOADS"] != nil, "run by scripts/relay_sim_e2e.sh"))
    func opensWhatTheRelayDelivered() async throws {
        let list = try #require(ProcessInfo.processInfo.environment["RELAY_PAYLOADS"])
        for item in list.split(separator: "|") {
            let parts = item.split(separator: "=", maxSplits: 1).map(String.init)
            let data = try Data(contentsOf: URL(fileURLWithPath: parts[0]))
            let info = try #require(try JSONSerialization.jsonObject(with: data) as? [String: Any])
            let shown = await Self.deliver(info)
            #expect(shown.body == parts[1], "\(parts[0])")
        }
    }
}
