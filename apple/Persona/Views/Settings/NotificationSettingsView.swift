#if canImport(ActivityKit)
import ActivityKit
#endif
import SwiftUI
import UserNotifications

/// Settings › Notifications: which pushes this iPhone gets. The hub keeps the choice per device (notify.go), so a second phone or
/// an iPad can choose differently; the system switch (allowed at all, sounds, lock screen) stays in iOS Settings.
struct NotificationSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var prefs: NotifyPrefs?
    @State private var system: UNAuthorizationStatus?
    #if os(iOS)
    @State private var activitiesAllowed = ActivityAuthorizationInfo().areActivitiesEnabled
    #endif
    @State private var debugIsland = false

    var body: some View {
        SettingsForm {
            if systemOff {
                Section {
                    Label {
                        VStack(alignment: .leading, spacing: Theme.Space.xs) {
                            Text("通知没有打开").font(Theme.Font.heading)
                            Text("下面的设置要在系统设置里允许 Persona 发通知后才生效。").font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: Theme.Icon.muted).foregroundStyle(Theme.caution)
                    }
                    openSystemSettings
                }
            }

            Section {
                Toggle(isOn: binding(\.input)) {
                    Label {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text("需要你处理时")
                            Text("提问、计划审批、工具授权、终端等你输入").font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "exclamationmark.bubble").foregroundStyle(StatusPalette.needsYou.dot)
                    }
                }
            } footer: {
                #if os(macOS)
                Text("授权和计划的通知上可以直接「允许 / 批准」或「拒绝 / 打回」（把鼠标移到通知上，点「选项」）。")
                #else
                Text("授权和计划的通知上可以直接「允许 / 批准」（要先解锁）或「拒绝 / 打回」。")
                #endif
            }

            Section {
                Picker(selection: binding(\.done)) {
                    Text("不推送").tag(NotifyPrefs.Done.off)
                    Text("只推长任务").tag(NotifyPrefs.Done.long)
                    Text("每次都推").tag(NotifyPrefs.Done.always)
                } label: {
                    Label("Agent 做完时", systemImage: Theme.Icon.success)
                }
                #if os(macOS)
                .pickerStyle(.radioGroup) // a Mac form's one-of-several (#1221)
                #else
                .pickerStyle(.inline)
                #endif
                .labelsHidden() // the section header says it
            } header: {
                Text("Agent 做完时")
            } footer: {
                Text(doneFooter)
            }

            // Live Activities (Lock Screen and Dynamic Island) are an iPhone's: a Mac has neither (docs/mac.md).
            #if os(iOS)
            Section {
                Toggle(isOn: binding(\.liveActivity)) {
                    Label {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text("锁屏和灵动岛")
                            Text("一个实时活动汇总需要你、在工作、做完了的 Agent").font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: Theme.Icon.jarvis).foregroundStyle(Theme.brand)
                    }
                }
                Toggle(isOn: binding(\.lockDetails)) {
                    Label {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text("锁屏显示详情")
                            Text("关着时锁屏只显示数量和「有个授权等你」，不显示 Agent 名字和内容").font(Theme.Font.caption).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: Theme.Icon.view).foregroundStyle(.secondary)
                    }
                }
                .disabled(prefs?.liveActivity == false)
                NavigationLink {
                    IslandSettingsView(island: binding(\.island))
                } label: {
                    LabeledContent {
                        Text((prefs?.island ?? IslandPrefs()).preset.title).lineLimit(1)
                    } label: {
                        Label("灵动岛", systemImage: "capsule.fill")
                    }
                }
                .disabled(prefs?.liveActivity == false)
                if !activitiesAllowed {
                    Label {
                        Text("系统设置里关掉了 Persona 的实时活动").font(Theme.Font.caption)
                    } icon: {
                        Image(systemName: Theme.Icon.errorFilled).foregroundStyle(Theme.caution)
                    }
                    Button {
                        UIApplication.shared.open(URL(string: UIApplication.openSettingsURLString)!)
                    } label: {
                        Label("打开系统设置", systemImage: "gear").lineLimit(1)
                    }
                }
            } header: {
                Text("实时活动")
            } footer: {
                Text("锁屏上可以直接「允许 / 批准」（要先解锁）或「拒绝」；灵动岛只在解锁时显示，总是带详情。")
            }
            #endif

            // The Mac had the button twice while notifications were off (#1224 A5): the warning above has it then.
            #if os(macOS)
            if !systemOff { systemSettingsSection }
            #else
            systemSettingsSection
            #endif
        }
        .themedPage()
        .detailTitle(String(localized: "通知"))
        .navigationBarTitleDisplayMode(.inline)
        .disabled(prefs == nil)
        .overlay { if prefs == nil { ProgressView() } }
        .task { await load() }
        #if DEBUG && os(iOS)
        // `-openIslandSettings YES` (with `-openNotificationSettings YES -tab settings`): Dynamic Island.
        .navigationDestination(isPresented: $debugIsland) { IslandSettingsView(island: binding(\.island)) }
        .task {
            if UserDefaults.standard.bool(forKey: "openIslandSettings") {
                try? await Task.sleep(for: .milliseconds(600))
                debugIsland = true
            }
        }
        #endif
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in
            Task { system = await UNUserNotificationCenter.current().notificationSettings().authorizationStatus }
            #if os(iOS)
            activitiesAllowed = ActivityAuthorizationInfo().areActivitiesEnabled
            #endif
        }
    }

    /// Allowed for a while (App Clips): iOS only.
    private static func ephemeral(_ status: UNAuthorizationStatus) -> Bool {
        #if os(iOS) || os(visionOS)
        status == .ephemeral
        #else
        false
        #endif
    }

    /// Notifications are not allowed for the app (the system's switch): the page says so first.
    private var systemOff: Bool {
        guard let system else { return false }
        return system != .authorized && system != .provisional && !Self.ephemeral(system)
    }

    private var systemSettingsSection: some View {
        Section {
            openSystemSettings
        } footer: {
            Text("声音、锁屏显示、专注模式在系统设置里改。")
        }
    }

    private var openSystemSettings: some View {
        Button {
            UIApplication.shared.open(URL(string: UIApplication.openNotificationSettingsURLString)!)
        } label: {
            Label("系统通知设置", systemImage: "gear").lineLimit(1)
        }
    }

    private var doneFooter: String {
        let delay = NotifyPrefs.duration(prefs?.doneDelaySeconds)
        let long = NotifyPrefs.duration(prefs?.longTurnSeconds)
        return String(localized: "做完后等 \(delay)，这期间在电脑上或 App 里看过就不推；后台任务、子 Agent 都结束后才推一次。「长任务」指干了 \(long)以上的那一轮，秒回的问题在哪问的就在哪看。")
    }

    private func binding<V>(_ path: WritableKeyPath<NotifyPrefs, V>) -> Binding<V> {
        Binding {
            (prefs ?? NotifyPrefs())[keyPath: path]
        } set: { value in
            guard var next = prefs else { return }
            next[keyPath: path] = value
            save(next)
        }
    }

    private func load() async {
        system = await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
        guard let api = model.api else { return }
        do {
            let p = try await api.notifyPrefs(device: PushRegistrar.deviceID)
            prefs = p
            LiveActivityController.shared.settings(p)
        } catch { model.show(error) }
    }

    /// Optimistic: the control moves at once and goes back if the hub says no.
    private func save(_ next: NotifyPrefs) {
        let before = prefs
        prefs = next
        LiveActivityController.shared.settings(next)
        guard let api = model.api else { return }
        Task {
            do {
                prefs = try await api.setNotifyPrefs(device: PushRegistrar.deviceID, next)
                Haptics.success()
            } catch {
                prefs = before
                if let before { LiveActivityController.shared.settings(before) }
                model.show(error)
            }
        }
    }
}
