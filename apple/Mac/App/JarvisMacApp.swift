import AppKit
import SwiftUI
import UserNotifications

/// The macOS app (docs/mac.md): one AppModel, as on iOS, behind a Mac shell. A window with a sidebar (the iPhone's
/// four tabs) beside the same pages, and 设置 also in its own window (⌘,).
@main
struct JarvisMacApp: App {
    @NSApplicationDelegateAdaptor(MacAppDelegate.self) private var delegate
    @State private var model = AppModel.shared
    @Environment(\.scenePhase) private var phase

    var body: some Scene {
        WindowGroup("Jarvis", id: "main") {
            MacRootView()
                .environment(model)
                .onOpenURL { url in model.openURL(url) }
                // A jarvis:// link goes to the window that is open: without this every link opened another main window.
                .handlesExternalEvents(preferring: ["*"], allowing: ["*"])
        }
        .handlesExternalEvents(matching: ["*"])
        .defaultSize(width: Theme.Size.macWindowWidth, height: Theme.Size.macWindowHeight)
        .windowToolbarStyle(.unified)
        .commands {
            MacCommands(model: model)
            MacAgentMenu(model: model)
            MacViewMenu(model: model)
        }
        .onChange(of: phase) { _, new in
            // A Mac app keeps running in the background: the socket stays open (the hub reaches it that way), only
            // coming back to the front checks the connection and refreshes, as the iPhone does.
            if new == .active {
                model.live.ensureConnected()
                Task { await model.refresh() }
            }
        }

        // An Agent's terminal on its own (Agent › 在新窗口打开终端, MacTerminalWindow).
        WindowGroup("终端", id: MacTerminalWindow.id, for: SessionKey.self) { $key in
            MacTerminalWindow(key: key).environment(model)
        }
        .defaultSize(width: Theme.Size.macWindowHeight, height: Theme.Size.macWindowMinHeight)

        // A conversation on its own (「在新窗口打开」, MacChatWindow).
        WindowGroup("对话", id: MacChatWindow.id, for: SessionKey.self) { $key in
            MacChatWindow(key: key).environment(model)
        }
        .defaultSize(width: Theme.Size.macWindowHeight, height: Theme.Size.macWindowHeight)

        // A link dragged out of the panel, an inbox item opened on its own (Mac/Link, Mac/Inbox).
        WindowGroup("网页", id: MacLinkWindow.id, for: URL.self) { $url in
            MacLinkWindow(url: url).environment(model)
        }
        .defaultSize(width: Theme.Size.macLinkPanelIdeal * 2, height: Theme.Size.macWindowHeight)
        WindowGroup("收件箱", id: MacInboxItemWindow.id, for: String.self) { $id in
            MacInboxItemWindow(itemID: id).environment(model)
        }
        .defaultSize(width: Theme.Size.macWindowHeight, height: Theme.Size.macWindowHeight)

        Settings {
            MacSettingsWindow()
                .environment(model)
        }

        MenuBarExtra {
            MacMenuBarMenu().environment(model)
        } label: {
            MacMenuBarLabel().environment(model)
        }
        .menuBarExtraStyle(.menu)
    }
}

extension AppModel {
    /// `jarvis://pair?…` (a link from `jarvisd hub pair`, or pasted): pair with that hub.
    /// A `jarvis://` link from outside (MacOpenURL): open a chat, or pair.
    func openURL(_ url: URL) {
        #if DEBUG
        if UserDefaults.standard.bool(forKey: "macWindowLog") { // how many main windows a link leaves (docs/mac.md)
            DispatchQueue.main.asyncAfter(deadline: .now() + 1) {
                let mains = NSApp.windows.filter { $0.identifier?.rawValue.hasPrefix("main") == true && $0.isVisible }
                NSLog("macWindowLog: %d main window(s) after %@", mains.count, url.absoluteString)
            }
        }
        #endif
        switch MacOpenURL.route(url) {
        case .session(let key): MacShellState.shared.open(key, self)
        case .pair: openPairing(url)
        case .app, .ignore: break
        }
    }

    func openPairing(_ url: URL) {
        guard let cfg = ServerConfig(pairURL: url.absoluteString) else { return }
        guard MacPairing.accepts(isPaired: isPaired, isDemo: isDemo) else {
            toast = Toast("已经连着 \(config?.url.host() ?? "一个 hub")：要换 hub，先在 设置 › 服务器 里断开", style: .error)
            return
        }
        Task {
            do { try await pair(cfg); toast = Toast("已连接到 \(cfg.url.host() ?? "服务器")", style: .success) }
            catch { show(error) }
        }
    }
}

/// The app's menus: the four sections on ⌘1…⌘4, as Mac apps with a sidebar do.
struct MacCommands: Commands {
    let model: AppModel

    var body: some Commands {
        // 文件: 新建 Agent 对话 ⌘N, 新建 Jarvis 对话 ⇧⌘N (docs/mac.md「菜单和快捷键」). Not while unpaired: nothing to start on.
        CommandGroup(replacing: .newItem) {
            Button("新建 Agent 对话…") { MacShellState.shared.newAgent = true }
                .keyboardShortcut("n", modifiers: .command)
                .disabled(!model.isPaired)
            Button("新建 Jarvis 对话") {
                MacShellState.shared.willOpen()
                model.selectedTab = .assistant
                model.assistantCurrent = nil
            }
            .keyboardShortcut("n", modifiers: [.command, .shift])
            .disabled(!model.isPaired)
        }
        // 前往: 快速打开 ⌘K.
        CommandGroup(after: .textEditing) {
            Button("查找…") { MacFind.find() }
                .keyboardShortcut("f", modifiers: .command)
        }
        CommandMenu("前往") {
            Button("快速打开…") { MacShellState.shared.quickOpen.toggle() }
                .keyboardShortcut("k", modifiers: .command)
                .disabled(!model.isPaired)
        }
        CommandGroup(after: .sidebar) {
            ForEach(Array(MacSection.allCases.enumerated()), id: \.element) { i, section in
                Button(section.title) { MacShellState.shared.go(section, model) }
                    .keyboardShortcut(KeyEquivalent(Character("\(i + 1)")), modifiers: .command)
            }
            Divider()
        }
    }
}

/// 设置 in the Settings window (⌘,): the same page as the sidebar's 设置.
struct MacSettingsWindow: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        NavigationStack {
            SettingsView()
        }
        .macLook(model)
        .frame(minWidth: Theme.Size.macSettingsWidth, minHeight: Theme.Size.macSettingsHeight)
    }
}

final class MacAppDelegate: NSObject, NSApplicationDelegate, UNUserNotificationCenterDelegate {
    func applicationWillFinishLaunching(_ notification: Notification) {
        #if DEBUG
        MacSnapshot.applyAppearanceIfAsked()
        #endif
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        UNUserNotificationCenter.current().delegate = self
        UNUserNotificationCenter.current().setNotificationCategories(NotificationActions.categories)
        #if DEBUG
        MacLaunchArguments.apply()
        #endif
        AppModel.shared.start()
        MacAttentionWatcher.shared.start(AppModel.shared)
        MacShellState.shared.watchKeyWindow()
        #if DEBUG
        MacSnapshot.scheduleIfAsked()
        #endif
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { false }

    func applicationDockMenu(_ sender: NSApplication) -> NSMenu? { MacDockMenu.shared.menu(AppModel.shared) }

    /// Clicking the Dock icon with no window open brings the main window back.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { sender.windows.first { $0.identifier?.rawValue.hasPrefix("main") == true }?.makeKeyAndOrderFront(nil) }
        return true
    }

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
        let info = response.notification.request.content.userInfo
        let key = Self.sessionKey(info)
        let action = response.actionIdentifier
        let text = (response as? UNTextInputNotificationResponse)?.userText
        let ixID = info["interaction_id"] as? String
        nonisolated(unsafe) let done = completionHandler
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                if let ixID, let resolution = NotificationActions.resolution(action: action, text: text) {
                    Task {
                        do {
                            guard let config = Keychain.serverConfig else { throw IntentFailure.notPaired }
                            try await APIClient(config: config).resolve(ixID, resolution)
                        } catch {
                            await NotificationActions.reportFailure(id: ixID, title: NotificationActions.failureTitle(action: action), error: error)
                        }
                        done()
                    }
                    return
                }
                if action != UNNotificationDismissActionIdentifier, let key {
                    MacShellState.shared.showMain() // the main window may have been closed
                    MacShellState.shared.open(key, AppModel.shared, latestAnswer: ixID == nil)
                }
                done()
            }
        }
    }

    nonisolated static func sessionKey(_ info: [AnyHashable: Any]) -> SessionKey? {
        guard let machine = info["machine_id"] as? String, let session = info["session_id"] as? String,
              !machine.isEmpty, !session.isEmpty else { return nil }
        return SessionKey(machineID: machine, sessionID: session)
    }
}

#if DEBUG
/// The iOS app's DEBUG launch arguments (`JarvisApp.swift`), read the same way: `-pairURL`, `-tab`, `-openSession`,
/// `-openAssistant`, `-newAssistant`, `-showNewChat`, `-openChatsFresh`, `-openAgentsFresh`, `-lark-connector`, `-simulateTap`.
/// `-demo.on YES` needs nothing here: AppModel reads it from the defaults.
enum MacLaunchArguments {
    @MainActor static func apply() {
        let d = UserDefaults.standard
        let model = AppModel.shared
        if let s = d.string(forKey: "pairURL"), let cfg = ServerConfig(pairURL: s) {
            Keychain.serverConfig = cfg
            model.adopt(cfg)
        }
        if d.bool(forKey: "openChatsFresh") { ChatCache.removeAll() }
        if d.bool(forKey: "openAgentsFresh") {
            for k in ["dev.machineFilter", "dev.grouping", "dev.compact", "dev.folded"] { d.removeObject(forKey: k) }
        }
        // A tab, a chat or a conversation named on the command line shows that, not the 知识库 / 机器 remembered
        // from last time (MacShell's onChange is not there yet to clear it).
        if ["tab", "openSession", "openAssistant", "newAssistant", "lark-connector"].contains(where: { d.object(forKey: $0) != nil }) {
            MacShellState.shared.section = nil
        }
        switch d.string(forKey: "tab") {
        case "dev": model.selectedTab = .chats
        case "mail": model.selectedTab = .mail
        case "settings": model.selectedTab = .settings
        case "assistant": model.selectedTab = .assistant
        default: break
        }
        if let key = sessionKey(d.string(forKey: "openSession")) {
            if d.bool(forKey: "openFromDoneNotice") {
                model.open(key, latestAnswer: true)
            } else {
                model.selectedTab = .chats
                model.openChat = key
            }
        }
        if d.string(forKey: "lark-connector") != nil { model.selectedTab = .mail }
        if let key = sessionKey(d.string(forKey: "openAssistant")) {
            model.selectedTab = .assistant
            model.assistantCurrent = key
        }
        if d.bool(forKey: "newAssistant") {
            model.selectedTab = .assistant
            model.assistantCurrent = nil
        }
        model.debugShowNewChat = d.bool(forKey: "showNewChat")
        // `-openQuickOpen <query>` (`""` for none): ⌘K open, that typed in. `-openNewAgent YES`: the 新建 Agent sheet.
        if let q = d.string(forKey: "openQuickOpen") {
            MacShellState.shared.seed = q
            MacShellState.shared.quickOpen = true
        }
        if d.bool(forKey: "openNewAgent") { MacShellState.shared.newAgent = true }
        // `-openTerminalWindow <machine>/<session>`: that Agent's terminal in a window of its own.
        if let key = sessionKey(d.string(forKey: "openTerminalWindow")) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1) { MacShellState.shared.terminalWindow = key }
        }
        // `-openChatWindow <machine>/<session>`: that conversation in a window of its own.
        if let key = sessionKey(d.string(forKey: "openChatWindow")) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1) { MacShellState.shared.chatWindow = key }
        }
        // `-macAgentAsk rename|clear|close|inspector` with `-openSession`: what that Agent menu item does, 3 s in.
        if let key = sessionKey(d.string(forKey: "openSession")), let ask = d.string(forKey: "macAgentAsk") {
            let kinds: [String: MacShellState.ChatRequest.Kind] = ["rename": .rename, "clear": .clear, "close": .close, "inspector": .inspector]
            if let kind = kinds[ask] {
                DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                    // For the main window, as the menu would with it key (`-openChatWindow` opens the same chat in a second one).
                    let main = NSApp.windows.first { $0.identifier?.rawValue.hasPrefix("main") == true }?.windowNumber
                    MacShellState.shared.chatRequest = .init(key: key, kind: kind, window: main)
                }
            }
        }
        // `-macOpenURL <jarvis://…>`: what handing the app that link does, 2 s in.
        if let u = d.string(forKey: "macOpenURL").flatMap(URL.init(string:)) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 2) { model.openURL(u) }
        }
        // `-macFind YES`: 编辑 › 查找… 3 s in, and who has the keyboard after it (in the log).
        if d.bool(forKey: "macFind") {
            DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                // A locked screen's snapshot run has no key window: use the main one, as ⌘F would.
                guard let w = NSApp.keyWindow ?? NSApp.windows.first(where: { $0.isVisible && $0.canBecomeMain }) else { return }
                let found = MacFind.focusSearch(in: w)
                let r = w.firstResponder
                NSLog("macFind: found %@", found ? "yes" : "no")
                NSLog("macFind: first responder %@ (delegate %@)", String(describing: r.map { type(of: $0) }),
                      String(describing: (r as? NSText)?.delegate.map { type(of: $0) }))
            }
        }
        // `-perfCounts YES -macPerfLog YES`: what hub pushes cost (PerfCount: bodies, main-thread ms), logged every 2 s,
        // for measuring against the mock hub's /mock/churn without a UI test (docs/mac.md「性能」).
        if PerfCount.enabled, d.bool(forKey: "macPerfLog") {
            Timer.scheduledTimer(withTimeInterval: 2, repeats: true) { _ in
                MainActor.assumeIsolated { NSLog("macPerf %@", PerfCount.summary) }
            }
        }
        // `-macSection knowledge|machines|…`: that sidebar section.
        if let s = d.string(forKey: "macSection").flatMap(MacSection.init(rawValue:)) { MacShellState.shared.go(s, model) }
        if let key = sessionKey(d.string(forKey: "simulateTap")) {
            DispatchQueue.main.asyncAfter(deadline: .now() + 4) { model.open(key) }
        }
    }

    /// `<machine>/<session>`.
    private static func sessionKey(_ s: String?) -> SessionKey? {
        guard let s, let slash = s.firstIndex(of: "/") else { return nil }
        return SessionKey(machineID: String(s[..<slash]), sessionID: String(s[s.index(after: slash)...]))
    }
}
#endif

/// Whether a `jarvis://pair` link (from the browser, `jarvisd hub pair`, Finder) may pair: not over a real pairing,
/// which a link from anywhere would otherwise replace without a word; 体验模式 makes way for a real hub.
enum MacPairing {
    static func accepts(isPaired: Bool, isDemo: Bool) -> Bool { !isPaired || isDemo }
}
