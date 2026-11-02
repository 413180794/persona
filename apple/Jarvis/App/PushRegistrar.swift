import Foundation
import UserNotifications
#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// Registers for APNs and tells the hub where to push (interactions, finished turns). The iPhone and the Mac app the
/// same way, as kind `iphone` / `mac`: same bundle id, so the same APNs topic. While the Mac app is connected the hub
/// leaves the agents' alerts to it (MacAttentionWatcher posts them); a Mac never gets the phone's wake pushes.
final class PushRegistrar {
    static let shared = PushRegistrar()

    func registerIfPossible() {
        guard AppModel.shared.isPaired, !AppModel.shared.isDemo else { return }
        #if DEBUG
        // Simulator and snapshot automation: no permission alert covering the screenshots.
        if UserDefaults.standard.string(forKey: "pairURL") != nil { return }
        #endif
        Task {
            // Asked here, not when the first notification is due: a push that comes while the app is closed shows
            // only if this was allowed before.
            let center = UNUserNotificationCenter.current()
            let granted = (try? await center.requestAuthorization(options: [.alert, .sound, .badge, .timeSensitive])) ?? false
            #if os(macOS)
            if granted { NSApplication.shared.registerForRemoteNotifications() }
            #else
            if granted { UIApplication.shared.registerForRemoteNotifications() }
            #endif
        }
    }

    func didRegister(token: Data) {
        let hex = token.map { String(format: "%02x", $0) }.joined()
        guard let api = AppModel.shared.api else { return }
        #if DEBUG
        let env = "sandbox"
        #else
        let env = "production"
        #endif
        #if os(macOS)
        let kind = "mac"
        #else
        let kind = "iphone"
        #endif
        let body = APIClient.DeviceBody(deviceID: Self.deviceID, name: UIDevice.current.name, kind: kind, apnsToken: hex,
                                        apnsEnv: env, topic: Bundle.main.bundleIdentifier)
        Task {
            try? await api.register(device: await Self.withRelay(body, token: hex, env: env))
        }
    }

    /// A hub without the app's APNs key pushes through the relay, sealed to this key (docs/push-relay.md). The hub
    /// decides: with its own key it ignores these and pushes directly, as before. Not on the Mac yet (#432): it has no
    /// notification service extension to open the sealed payload, so it would only ever show 「有新消息」.
    private static func withRelay(_ body: APIClient.DeviceBody, token: String, env: String) async -> APIClient.DeviceBody {
        #if os(iOS)
        var body = body
        if let relay = PushRelay.url, let key = PushCrypto.ensureKey(), let topic = body.topic {
            do {
                body.relayGrant = try await PushRelay.grant(relay: relay, token: token, env: env, topic: topic)
                body.relayURL = relay.absoluteString
                body.pushKey = PushCrypto.publicKey(key)
            } catch {
                PushRelay.log.error("relay registration: \(error.localizedDescription, privacy: .public)")
            }
        }
        #endif
        return body
    }

    /// This device's id at the hub: its push token and its notification settings hang off it.
    static var deviceID: String {
        if let id = UserDefaults.standard.string(forKey: "deviceID") { return id }
        let id = UUID().uuidString
        UserDefaults.standard.set(id, forKey: "deviceID")
        return id
    }
}
