import SwiftUI
#if canImport(UIKit)
import UIKit
#endif
import UserNotifications

@main
struct PersonaApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @State private var model = AppModel.shared
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(model)
                .task(id: model.fleetLook) { LiveActivityController.shared.look(model.fleetLook) { model.fleetFaces } }
                .onOpenURL { url in
                    #if os(iOS)
                    if let link = FleetLink.session(url) { // tapped the Live Activity
                        // "group|machine" for another group's row (FleetMerge); a row the hub pushed names no group.
                        if let key = model.resolve(path: link.machine + "/" + link.session) { model.open(key) }
                        return
                    }
                    #endif
                    guard let cfg = ServerConfig(pairURL: url.absoluteString) else { return }
                    Task {
                        do { try await model.pair(cfg); model.toast = Toast("已连接到 \(cfg.url.host() ?? "服务器")", style: .success) }
                        catch { model.show(error) }
                    }
                }
        }
        #if os(visionOS)
        .defaultSize(width: Theme.Size.visionMainWindowWidth, height: Theme.Size.visionMainWindowHeight)
        #endif
        // `initial`: a launch into the foreground is already active here, and `start()` may have found the app in the
        // background (scene apps are, during didFinishLaunching): the socket opens now.
        .onChange(of: phase, initial: true) { old, new in
            if new == .active {
                model.retryRegistry() // launched in the background before the first unlock: the pairing reads now
                model.resumeLinks()
                LiveActivityController.shared.look(model.fleetLook) { model.fleetFaces }
                // The home screen icon in step with 深浅色 (a scheme from a launch argument, a switch the system cut short);
                // nothing happens when it already is, so no alert.
                Task { await model.appIcon.follow(model.colorScheme) }
                if old != new { Task { await model.refresh() } }
            } else if new == .background {
                // A suspended app's socket looks alive to the hub until it times out, and phone_* calls sent to it hang.
                // Closed, the hub knows the phone is away and wakes it with a silent push instead; and nothing opens it
                // again until the app is back (the network returning, a refresh ending: LiveConnection.suspended).
                model.suspendLinks()
            }
        }
        #if os(visionOS)
        // A conversation opened in a window of its own (VisionScenes).
        VisionChatScene(model: model)
        #endif
    }
}

final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        _ = KeyboardState.shared // watches the keyboard from the start: a card may come while it is already up
        UNUserNotificationCenter.current().delegate = self
        UNUserNotificationCenter.current().setNotificationCategories(NotificationActions.categories)
        #if DEBUG
        // Simulator automation: `simctl launch … -pairURL jarvis://pair?...`; `-pairURL2`, `-pairURL3` pair more hubs
        // (several groups, docs/multi-hub.md「截图」).
        if let s = UserDefaults.standard.string(forKey: "pairURL"), let cfg = ServerConfig(pairURL: s) {
            let more = ["pairURL2", "pairURL3"].compactMap { UserDefaults.standard.string(forKey: $0).flatMap(ServerConfig.init(pairURL:)) }
            AppModel.shared.adopt([cfg] + more)
        }
        let d = UserDefaults.standard
        // `-openPairLink <persona://pair?…>`: pairs as a scanned link does (`pair`, then the device token trade), unlike
        // `-pairURL`; `-openPairLinkTimes 2` handles the same link again a few seconds later (#2766).
        if let s = d.string(forKey: "openPairLink"), let cfg = ServerConfig(pairURL: s) {
            let times = max(1, d.integer(forKey: "openPairLinkTimes"))
            Task { @MainActor in
                for i in 0..<times {
                    if i > 0 { try? await Task.sleep(for: .seconds(3)) }
                    do { try await AppModel.shared.pair(cfg) } catch { AppModel.shared.show(error) }
                }
            }
        }
        PopFrames.startIfAsked()
        // `-openChatsFresh YES`: no chat kept on disk from an earlier run (LiveUpdateTests: churned steps piled up).
        if d.bool(forKey: "openChatsFresh") { ChatCache.removeAll() }
        // `-openAgentsFresh YES`: the Agents list's saved look forgotten (AgentsListTests).
        if d.bool(forKey: "openAgentsFresh") {
            for k in ["dev.machineFilter", HubFilter.key, "dev.grouping", "dev.sort", "dev.compact", "dev.folded"] { d.removeObject(forKey: k) }
        }
        // `-openRecapsFresh YES`: no away recap closed yet, so the newest shows open again (RecapInFlowTests).
        if d.bool(forKey: "openRecapsFresh") { d.removeObject(forKey: "closedRecaps") }
        // `-openScreenFresh YES`: no Mac's Screen Sharing sign-in remembered, 一键解锁 off (ScreenPasswordTests, #1322).
        if d.bool(forKey: "openScreenFresh") {
            ScreenCredentials.forgetAll()
            ScreenUnlock.forgetAll()
        }
        // `-lark-connectorSendFresh YES`: no Feishu draft and no outbox (unsent, failed messages) from an earlier run:
        // LarkSendTests found earlier runs' failed messages and drafts in the chat (#1627).
        if d.bool(forKey: "lark-connectorSendFresh") {
            StateCache.remove(LarkDrafts.stateName)
            for dir in ["lark-outbox-v2", "lark-outbox-v1"] {
                try? FileManager.default.removeItem(at: URL.applicationSupportDirectory.appending(path: dir))
            }
        }
        #if os(iOS) || os(visionOS)
        PagePerf.runIfAsked()
        #endif
        // Every tab a UI test can ask for. One it asks for and this does not know leaves the tab the last run saved
        // (`app.tab`): after a run in the old layout with `-tab settings`, the shell opened on the 设置 sheet and the
        // Feishu tests that asked for `-tab feishu` never saw the 消息 page (#1664).
        switch d.string(forKey: "tab") {
        case "dev": AppModel.shared.selectedTab = .chats
        case "mail": AppModel.shared.selectedTab = .mail
        case "settings": AppModel.shared.selectedTab = .settings
        case "assistant": AppModel.shared.selectedTab = .assistant
        case "feishu": AppModel.shared.selectedTab = LarkHomePreference.shared.shows ? .feishu : .assistant
        default: break
        }
        // "machine/session", or "group|machine/session" for another group.
        if let open = d.string(forKey: "openSession"), let key = AppModel.shared.resolve(path: open) {
            // `-openFromDoneNotice YES`: as a tap on its "完成" notification (lands on the latest answer).
            if d.bool(forKey: "openFromDoneNotice") {
                AppDelegate.openFromNotification(key, finished: true)
            } else {
                AppModel.shared.selectedTab = .chats
                AppModel.shared.openChat = key
            }
        }
        if d.string(forKey: "lark-connector") != nil { AppModel.shared.selectedTab = .mail }
        if let open = d.string(forKey: "openAssistant"), let key = AppModel.shared.resolve(path: open) {
            AppModel.shared.selectedTab = .assistant
            AppModel.shared.assistantCurrent = key
        }
        // `-openHub <group id or 1-based index>`: the hub-level pages show that group (several groups).
        if let g = d.string(forKey: "openHub") {
            let ids = AppModel.shared.groupIDs
            AppModel.shared.selectHub(Int(g).flatMap { ids.indices.contains($0 - 1) ? ids[$0 - 1] : nil } ?? g)
        }
        if d.bool(forKey: "newAssistant") {
            AppModel.shared.selectedTab = .assistant
            AppModel.shared.assistantCurrent = nil
        }
        AppModel.shared.debugShowNewChat = d.bool(forKey: "showNewChat")
        // `-simulateTap <machine>/<session>`: what tapping a push does, 4 s after launch (on another screen by then).
        if let tap = d.string(forKey: "simulateTap"), let key = AppModel.shared.resolve(path: tap) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 4) { AppDelegate.openFromNotification(key) }
        }
        // `-showTestNotification permission|plan`: posts what an interaction push looks like (category, ids), 3 s
        // after the app goes to the background, so the UI test can exercise the notification buttons without APNs.
        if let kind = d.string(forKey: "showTestNotification").flatMap(InteractionKind.init(rawValue:)) {
            Task { await AppDelegate.postTestNotification(kind) }
        }
        // `-showPushKey YES`: asks for notifications and writes this phone's push public key to Caches/push-key.txt, so
        // scripts/test/relay_sim_e2e.sh can seal a push to it and send it through a local relay (docs/push-relay.md).
        if d.bool(forKey: "showPushKey") {
            Task {
                _ = try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])
                guard let key = PushCrypto.ensureKey(),
                      let dir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first else { return }
                try? Data(PushCrypto.publicKey(key).utf8).write(to: dir.appending(path: "push-key.txt"))
            }
        }
        if let phase = d.string(forKey: "showFleetSample") {
            LiveActivityController.shared.showSample(phase, details: d.bool(forKey: "fleetDetails"), island: d.string(forKey: "fleetIsland"),
                                                     count: d.integer(forKey: "showFleetCount"))
        }
        RuntimeCLI.startIfAsked() // `-runtimeCLI <port>`: scripts/app/jarvisctl.py drives the running app
        let automation = d.string(forKey: "pairURL") != nil
        #else
        let automation = false
        #endif
        PhoneRecordingReceiver.shared.start()
        WatchSync.shared.activate()
        AppModel.shared.start()
        LiveActivityController.shared.start()
        if !automation { PushRegistrar.shared.registerIfPossible() }
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        PushRegistrar.shared.didRegister(token: deviceToken)
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {}

    /// Silent push from the hub (`jarvis_wake`): the assistant needs a phone_* tool while the app is in the background.
    /// iOS gives ~30 s; connect, let the hub deliver the call over the socket, then let go.
    func application(_ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any],
                     fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void) {
        // Through the relay the reason is sealed (docs/push-relay.md): open it here, in the app's own process, with
        // each group's key (the one that opens it says whose hub needs the phone).
        var info = userInfo
        if PushCrypto.isSealed(userInfo), let opened = PushCrypto.opened(userInfo, keys: PushCrypto.allKeys()) {
            for (k, v) in opened.payload { info[k] = v }
            if !opened.group.isEmpty { info[PushCrypto.groupKey] = opened.group }
        }
        let wake = info["jarvis_wake"]
        guard wake != nil, AppModel.shared.isPaired, !AppModel.shared.isDemo else {
            completionHandler(.noData)
            return
        }
        // That group's hub (`hub_id`, the key); unknown: every group.
        let group = AppModel.shared.pushGroup(info)
        AppModel.shared.wakeLinks(group)
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.asyncAfter(deadline: .now() + 25) {
            if UIApplication.shared.applicationState == .background { AppModel.shared.suspendLinks() }
            done(.newData)
        }
    }

    // Completion-handler versions on purpose: the async variants of these delegate methods return on a background
    // executor, so UIKit gets its completion off the main thread and crashes when a tap opens the app. Hop to the main
    // queue, do the work there and call back from there.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                            withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        let ref = Self.sessionRef(notification.request.content.userInfo)
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            let (key, visible, quiet) = MainActor.assumeIsolated {
                let key = ref.flatMap(Self.sessionKey)
                // A quiet task's agent (#1731) is the main conversation's to tell; a hub that knows sends no push for it.
                return (key, AppModel.shared.visibleSession, key.map(AppModel.shared.isQuiet) ?? false)
            }
            done(key != nil && (key == visible || quiet) ? [] : [.banner, .sound, .list])
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler completionHandler: @escaping () -> Void) {
        let info = response.notification.request.content.userInfo
        let ref = Self.sessionRef(info)
        let action = response.actionIdentifier
        let text = (response as? UNTextInputNotificationResponse)?.userText
        let ixID = info["interaction_id"] as? String
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                // The group the push came from (docs/multi-hub.md「推送」): its hub settles the button, its session opens.
                let key = ref.flatMap(Self.sessionKey)
                if let ixID, let resolution = NotificationActions.resolution(action: action, text: text) {
                    // A button on the notification: settle it in the background, the app stays where it is.
                    AppDelegate.resolveFromNotification(ixID, group: key?.group ?? ref?.group, resolution, action: action, done: done)
                    return
                }
                // No interaction: a turn finished ("完成"), opened at its answer like from the list.
                if action != UNNotificationDismissActionIdentifier { AppDelegate.openFromNotification(key, finished: ixID == nil) }
                done()
            }
        }
    }

    /// Runs a notification button against the hub. iOS may have launched the app in the background just for this, so
    /// hold a background task until the request is answered; a failure comes back as a local notification, because
    /// the user is not looking at the app.
    static func resolveFromNotification(_ id: String, group: String?, _ resolution: Resolution, action: String, done: @escaping () -> Void) {
        let app = UIApplication.shared
        var task = UIBackgroundTaskIdentifier.invalid
        task = app.beginBackgroundTask(withName: "resolve-interaction") {
            app.endBackgroundTask(task)
            task = .invalid
        }
        Task {
            do {
                try await HubStore.client(group: group).resolve(id, resolution)
            } catch {
                await NotificationActions.reportFailure(id: id, title: NotificationActions.failureTitle(action: action), error: error)
            }
            done()
            if task != .invalid { app.endBackgroundTask(task) }
        }
    }

    #if DEBUG
    static func postTestNotification(_ kind: InteractionKind) async {
        let center = UNUserNotificationCenter.current()
        _ = try? await center.requestAuthorization(options: [.alert, .sound])
        // The answer can come back "not granted" while the alert is still being tapped: wait for the setting itself.
        for _ in 0..<60 where await center.notificationSettings().authorizationStatus != .authorized {
            try? await Task.sleep(for: .milliseconds(500))
        }
        // Delivered while the app is in the background, like a real push.
        for await _ in NotificationCenter.default.notifications(named: UIApplication.didEnterBackgroundNotification) { break }
        let content = UNMutableNotificationContent()
        content.title = kind == .plan ? "计划等你审批" : "需要你授权"
        content.subtitle = "测试会话 · 测试机器"
        content.body = kind == .plan ? "支持 HEIC" : "Bash: swift test"
        content.categoryIdentifier = NotificationActions.category(kind)
        content.userInfo = ["interaction_id": "ix_test_\(kind.rawValue)", "machine_id": "test", "session_id": "test"]
        try? await center.add(UNNotificationRequest(identifier: "test-\(kind.rawValue)", content: content,
                                                     trigger: UNTimeIntervalNotificationTrigger(timeInterval: 3, repeats: false)))
    }
    #endif

    nonisolated static func sessionRef(_ info: [AnyHashable: Any]) -> PushRef? { PushRef(info) }
    static func sessionKey(_ ref: PushRef) -> SessionKey { ref.key(AppModel.shared) }


    /// Opens the session a notification is about. On a cold launch the tap arrives before the UI exists: `open`
    /// only sets state (tab + chat to open) that the views pick up when they appear.
    static func openFromNotification(_ key: SessionKey?, finished: Bool = false) {
        guard let key else { return }
        AppModel.shared.open(key, latestAnswer: finished)
    }
}
