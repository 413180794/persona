import SwiftUI
import UserNotifications

/// 设置 › 通知: which pushes this iPhone gets. The hub keeps the choice per device (notify.go), so a second phone or
/// an iPad can choose differently; the system switch (allowed at all, sounds, lock screen) stays in iOS Settings.
struct NotificationSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var prefs: NotifyPrefs?
    @State private var system: UNAuthorizationStatus?

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
        do { prefs = try await api.notifyPrefs(device: PushRegistrar.deviceID) } catch { model.show(error) }
    }

    /// Optimistic: the control moves at once and goes back if the hub says no.
    private func save(_ next: NotifyPrefs) {
        let before = prefs
        prefs = next
        guard let api = model.api else { return }
        Task {
            do {
                prefs = try await api.setNotifyPrefs(device: PushRegistrar.deviceID, next)
                Haptics.success()
            } catch {
                prefs = before
                model.show(error)
            }
        }
    }
}
