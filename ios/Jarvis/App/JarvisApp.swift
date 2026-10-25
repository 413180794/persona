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
        case "dev": AppModel.shared.selectedTab = .chats
        case "mail": AppModel.shared.selectedTab = .mail
        case "settings": AppModel.shared.selectedTab = .settings
        default: break
        }
        if let open = d.string(forKey: "openSession"), let slash = open.firstIndex(of: "/") {
            AppModel.shared.selectedTab = .chats
            AppModel.shared.openChat = SessionKey(machineID: String(open[..<slash]), sessionID: String(open[open.index(after: slash)...]))
        }
        if d.string(forKey: "lark-connector") != nil { AppModel.shared.selectedTab = .mail }
        if let open = d.string(forKey: "openAssistant"), let slash = open.firstIndex(of: "/") {
            AppModel.shared.assistantCurrent = SessionKey(machineID: String(open[..<slash]), sessionID: String(open[open.index(after: slash)...]))
        }
        if d.bool(forKey: "newAssistant") { AppModel.shared.assistantCurrent = nil }
        AppModel.shared.debugShowNewChat = d.bool(forKey: "showNewChat")
        // `-simulateTap <machine>/<session>`: what tapping a push does, 4 s after launch (on another screen by then).
        if let tap = d.string(forKey: "simulateTap"), let slash = tap.firstIndex(of: "/") {
            let key = SessionKey(machineID: String(tap[..<slash]), sessionID: String(tap[tap.index(after: slash)...]))
            DispatchQueue.main.asyncAfter(deadline: .now() + 4) { AppDelegate.openFromNotification(key) }
        }
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

    // Completion-handler versions on purpose: the async variants of these delegate methods return on a background
    // executor, so UIKit gets its completion off the main thread and crashes when a tap opens the app. Hop to the main
    // queue, do the work there and call back from there.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                            withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        let key = Self.sessionKey(notification.request.content.userInfo)
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            let visible = MainActor.assumeIsolated { AppModel.shared.visibleSession }
            done(key != nil && key == visible ? [] : [.banner, .sound, .list])
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler completionHandler: @escaping () -> Void) {
        let key = Self.sessionKey(response.notification.request.content.userInfo)
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            MainActor.assumeIsolated { AppDelegate.openFromNotification(key) }
            done()
        }
    }

    nonisolated static func sessionKey(_ info: [AnyHashable: Any]) -> SessionKey? {
        guard let machine = info["machine_id"] as? String, let session = info["session_id"] as? String,
              !machine.isEmpty, !session.isEmpty else { return nil }
        return SessionKey(machineID: machine, sessionID: session)
    }

    /// Opens the session a notification is about. On a cold launch the tap arrives before the UI exists: `open`
    /// only sets state (tab + chat to open) that the views pick up when they appear.
    static func openFromNotification(_ key: SessionKey?) {
        guard let key else { return }
        AppModel.shared.open(key)
    }
}

/// Registers for APNs and tells the hub where to push (interactions, finished turns).
final class PushRegistrar {
    static let shared = PushRegistrar()

    func registerIfPossible() {
        guard AppModel.shared.isPaired else { return }
        #if DEBUG
        // Simulator automation: no permission alert covering the screenshots.
        if UserDefaults.standard.string(forKey: "pairURL") != nil { return }
        #endif
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
