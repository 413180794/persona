import ActivityKit
import SwiftUI
import UserNotifications

/// 设置 › 通知: which pushes this iPhone gets. The hub keeps the choice per device (notify.go), so a second phone or
/// an iPad can choose differently; the system switch (allowed at all, sounds, lock screen) stays in iOS Settings.
struct NotificationSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var prefs: NotifyPrefs?
    @State private var system: UNAuthorizationStatus?
    @State private var activitiesAllowed = ActivityAuthorizationInfo().areActivitiesEnabled

    var body: some View {
        List {
            if let system, system != .authorized && system != .provisional && system != .ephemeral {
                Section {
                    Label {
                        VStack(alignment: .leading, spacing: 4) {
                            Text("通知没有打开").font(.body.weight(.semibold))
                            Text("下面的设置要在系统设置里允许 Jarvis 发通知后才生效。").font(.footnote).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "bell.slash.fill").foregroundStyle(Theme.attention)
                    }
                    openSystemSettings
                }
            }

            Section {
                Toggle(isOn: binding(\.input)) {
                    Label {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("需要你处理时")
                            Text("提问、计划审批、工具授权、终端等你输入").font(.footnote).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "exclamationmark.bubble").foregroundStyle(Theme.attention)
                    }
                }
            } footer: {
                Text("授权和计划的通知上可以直接「允许 / 批准」（要先解锁）或「拒绝 / 打回」。")
            }

            Section {
                Picker(selection: binding(\.done)) {
                    Text("不推送").tag(NotifyPrefs.Done.off)
                    Text("只推长任务").tag(NotifyPrefs.Done.long)
                    Text("每次都推").tag(NotifyPrefs.Done.always)
                } label: {
                    Label("Agent 做完时", systemImage: "checkmark.circle")
                }
                .pickerStyle(.inline)
                .labelsHidden() // the section header says it
            } header: {
                Text("Agent 做完时")
            } footer: {
                Text(doneFooter)
            }

            Section {
                Toggle(isOn: binding(\.liveActivity)) {
                    Label {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("锁屏和灵动岛")
                            Text("一个实时活动汇总需要你、在工作、做完了的 Agent").font(.footnote).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "sparkle").foregroundStyle(Theme.accent(.claude))
                    }
                }
                Toggle(isOn: binding(\.lockDetails)) {
                    Label {
                        VStack(alignment: .leading, spacing: 2) {
                            Text("锁屏显示详情")
                            Text("关着时锁屏只显示数量和「有个授权等你」，不显示 Agent 名字和内容").font(.footnote).foregroundStyle(.secondary)
                        }
                    } icon: {
                        Image(systemName: "eye").foregroundStyle(.secondary)
                    }
                }
                .disabled(prefs?.liveActivity == false)
                if !activitiesAllowed {
                    Label {
                        Text("系统设置里关掉了 Jarvis 的实时活动").font(.footnote)
                    } icon: {
                        Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(Theme.attention)
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

            Section {
                openSystemSettings
            } footer: {
                Text("声音、锁屏显示、专注模式在系统设置里改。")
            }
        }
        .themedPage()
        .navigationTitle("通知")
        .navigationBarTitleDisplayMode(.inline)
        .disabled(prefs == nil)
        .overlay { if prefs == nil { ProgressView() } }
        .task { await load() }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in
            Task { system = await UNUserNotificationCenter.current().notificationSettings().authorizationStatus }
            activitiesAllowed = ActivityAuthorizationInfo().areActivitiesEnabled
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
        return "做完后等 \(delay)，这期间在电脑上或 App 里看过就不推；后台任务、子 Agent 都结束后才推一次。"
            + "「长任务」指干了 \(long)以上的那一轮，秒回的问题在哪问的就在哪看。"
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
            LiveActivityController.shared.settings(enabled: p.liveActivity, lockDetails: p.lockDetails)
        } catch { model.show(error) }
    }

    /// Optimistic: the control moves at once and goes back if the hub says no.
    private func save(_ next: NotifyPrefs) {
        let before = prefs
        prefs = next
        LiveActivityController.shared.settings(enabled: next.liveActivity, lockDetails: next.lockDetails)
        guard let api = model.api else { return }
        Task {
            do {
                prefs = try await api.setNotifyPrefs(device: PushRegistrar.deviceID, next)
                Haptics.success()
            } catch {
                prefs = before
                if let before { LiveActivityController.shared.settings(enabled: before.liveActivity, lockDetails: before.lockDetails) }
                model.show(error)
            }
        }
    }
}
