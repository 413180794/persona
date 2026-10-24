import SwiftUI
import UIKit
import UserNotifications

@main
struct JarvisApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var model = AppModel.shared
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .tint(Theme.accent)
                .onOpenURL { url in
                    guard let cfg = ServerConfig(pairURL: url.absoluteString) else { return }
                    Task {
                        do { try await model.pair(cfg); model.toast = Toast("已连接到 \(cfg.url.host() ?? "服务器")", style: .success) }
                        catch { model.show(error) }
                    }
                }
        }
        .onChange(of: phase) { _, new in
            if new == .active {
                model.live.ensureConnected()
                Task { await model.refresh() }
            }
        }
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        UNUserNotificationCenter.current().delegate = self
        #if DEBUG
        // Simulator automation: `simctl launch … -pairURL jarvis://pair?...`
        if let s = UserDefaults.standard.string(forKey: "pairURL"), let cfg = ServerConfig(pairURL: s) {
            Keychain.serverConfig = cfg
            AppModel.shared.adopt(cfg)
        }
        let d = UserDefaults.standard
        switch d.string(forKey: "tab") {
        case "machines": AppModel.shared.selectedTab = .machines
        case "mail": AppModel.shared.selectedTab = .mail
        case "settings": AppModel.shared.selectedTab = .settings
        default: break
        }
        if let open = d.string(forKey: "openSession"), let slash = open.firstIndex(of: "/") {
            AppModel.shared.openChat = SessionKey(machineID: String(open[..<slash]), sessionID: String(open[open.index(after: slash)...]))
        }
        AppModel.shared.debugShowNewChat = d.bool(forKey: "showNewChat")
        let automation = d.string(forKey: "pairURL") != nil
        #else
        let automation = false
        #endif
        WatchSync.shared.activate()
        AppModel.shared.start()
        if !automation { PushRegistrar.shared.registerIfPossible() }
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        PushRegistrar.shared.didRegister(token: deviceToken)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {}

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        let info = notification.request.content.userInfo
        let machine = info["machine_id"] as? String
        let session = info["session_id"] as? String
        let visible = await MainActor.run { AppModel.shared.visibleSession }
        if let machine, let session, visible == SessionKey(machineID: machine, sessionID: session) { return [] }
        return [.banner, .sound, .list]
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let info = response.notification.request.content.userInfo
        guard let machine = info["machine_id"] as? String, let session = info["session_id"] as? String else { return }
        await MainActor.run {
            AppModel.shared.selectedTab = .chats
            AppModel.shared.openChat = SessionKey(machineID: machine, sessionID: session)
        }
    }
}

/// Registers for APNs and tells the hub where to push (interactions, finished turns).
final class PushRegistrar {
    static let shared = PushRegistrar()

    func registerIfPossible() {
        guard AppModel.shared.isPaired else { return }
        Task {
            let center = UNUserNotificationCenter.current()
            let granted = (try? await center.requestAuthorization(options: [.alert, .sound, .badge, .timeSensitive])) ?? false
            if granted { UIApplication.shared.registerForRemoteNotifications() }
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
        let body = APIClient.DeviceBody(deviceID: deviceID, name: UIDevice.current.name, kind: "iphone", apnsToken: hex,
                                        apnsEnv: env, topic: Bundle.main.bundleIdentifier)
        Task { try? await api.register(device: body) }
    }

    private var deviceID: String {
        if let id = UserDefaults.standard.string(forKey: "deviceID") { return id }
        let id = UUID().uuidString
        UserDefaults.standard.set(id, forKey: "deviceID")
        return id
    }
}
