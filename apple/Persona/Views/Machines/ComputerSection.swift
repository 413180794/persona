import SwiftUI

/// 操控电脑 on a Mac's page while the hub's plugin cua is on (docs/computer-use.md): the switch that lets the assistant
/// operate it through Cua Driver, and whether it can now. What is missing comes with what to do about it: 安装 Cua Driver
/// when it is not there, 去授权 when a permission is missing (macOS asks on the Mac's own screen), with the node's
/// progress while it works. The node is asked again when the page opens, and every few seconds while a job runs.
struct ComputerSection: View {
    @Environment(AppModel.self) private var model
    let ref: MachineRef
    var computer: MachineComputer
    var online: Bool
    /// What the hub said since the page opened (newer than the machine list's).
    @State private var fresh: MachineComputer?
    @State private var saving = false
    @State private var asking = false
    @State private var checking = false
    /// This device remembers the Mac's Screen Sharing sign-in (#1322), and unlocks it with one tap (#1507 a-2).
    @State private var screenKept = false
    @State private var oneTap = false
    @State private var askOneTap = false
    /// The apps always allowed for the assistant here (#1507).
    @State private var apps: [ComputerApp] = []

    private var shown: MachineComputer { fresh ?? computer }

    /// Asked again every few seconds: a job runs, or macOS's dialogs wait on the Mac after a grant.
    private var watching: Bool {
        guard online, shown.enabled, let job = shown.job else { return false }
        return job.running || (job.kind == "grant" && job.state == "done" && !shown.missingPermissions.isEmpty)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            Text("操控电脑").font(Theme.Font.heading).padding(.horizontal, Theme.Space.xs)
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                Toggle(isOn: Binding(get: { shown.enabled }, set: { on in Task { await set(on) } })) {
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text("允许助手操控这台电脑").font(Theme.Font.body).lineLimit(1).minimumScaleFactor(0.8)
                        Text("经 Cua Driver 看屏幕、点按、输入、截图").font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                }
                .disabled(saving || !online)
                .accessibilityIdentifier("computer-switch")
                if shown.enabled {
                    Divider()
                    state
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .card()
        }
        .task(id: online && shown.enabled) {
            if online && shown.enabled { await refresh() }
        }
        .task(id: watching) {
            while watching, !Task.isCancelled {
                try? await Task.sleep(for: .seconds(2))
                await refresh()
            }
        }
        .task(id: model.screenOpen == nil) { // the drawer may have saved the sign-in, or unlocked the Mac
            screenKept = ScreenCredentials.load(ref) != nil
            oneTap = ScreenUnlock.isOn(ref)
            if model.screenOpen == nil, online, shown.enabled { await refresh() }
        }
        .confirmationDialog("打开\(Self.oneTapTitle)？", isPresented: $askOneTap, titleVisibility: .visible) {
            Button("打开") {
                ScreenUnlock.set(true, for: ref)
                oneTap = true
            }
            Button("取消", role: .cancel) {}
        } message: {
            Text("这台 Mac 锁屏时，点「一键解锁」并验证面容（或设备密码），就把记住的登录密码输入它的锁屏。解开后它不会自己再锁上，用完点「锁上」。")
        }
    }

    // MARK: State

    @ViewBuilder private var state: some View {
        let c = shown
        if !online {
            line("机器离线，连上后才能操控", ok: false)
        } else if let job = c.job, job.running, job.kind == "grant" {
            grantGuide(c, asking: true)
        } else if let job = c.job, job.running {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                HStack(spacing: Theme.Space.s) {
                    ProgressView().controlSize(.small)
                    Text(job.step ?? "正在安装")
                        .font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true)
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("computer-status")
                if job.kind == "install" { action("取消", "cancel", identifier: "computer-cancel") }
            }
        } else if !c.installed {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                if let job = c.job, job.kind == "install", job.state == "failed" {
                    line("没装上：\(job.error ?? "安装器出错了")", ok: false)
                } else {
                    line("这台 Mac 上还没有装 Cua Driver", ok: false)
                }
                Text("从 Cua 官网下载安装器，校验后装进「应用程序」，并关掉它的使用统计。")
                    .font(Theme.Font.label).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                action(c.job?.state == "failed" ? "重新安装" : "安装 Cua Driver", "install", prominent: true, identifier: "computer-install")
            }
        } else if c.desktop == "locked" {
            // Locked: nothing gets through to its apps (Cua refuses, screen_locked), so unlocking comes first. Its live
            // screen shows the lock screen, where the password is typed (docs/computer-use.md「锁屏」).
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                line("这台 Mac 锁屏了，解锁后才能操控", ok: false)
                if !c.screenSharing {
                    Text("在那台 Mac 上解锁；打开它的「屏幕共享」后，也能在这里用 VNC 远程解锁。")
                        .font(Theme.Font.label).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                } else if oneTap {
                    Text("点「一键解锁」，验证后把记住的密码输入它的锁屏。")
                        .font(Theme.Font.label).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    HStack(spacing: Theme.Space.s) {
                        Button { Task { await unlockNow() } } label: {
                            Label("一键解锁", systemImage: Theme.Icon.locked).font(Theme.Font.labelStrong).lineLimit(1)
                        }
                        .themedButton(prominent: true)
                        .accessibilityIdentifier("computer-unlock-now")
                        look(prominent: false)
                    }
                } else {
                    Text("点「解锁」打开它的画面，切到「接管」，在锁屏上输那台 Mac 的登录密码。")
                        .font(Theme.Font.label).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    Button { model.openScreen(ref) } label: {
                        Label("解锁", systemImage: Theme.Icon.screen).font(Theme.Font.labelStrong).lineLimit(1)
                    }
                    .themedButton(prominent: true)
                    .accessibilityIdentifier("computer-unlock")
                }
                if c.screenSharing, screenKept { unlockRow(locked: true) }
            }
        } else if c.daemon, !c.missingPermissions.isEmpty {
            grantGuide(c, asking: false)
        } else if c.ready {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                line("现在可以操控" + (c.version.map { " · Cua Driver \($0)" } ?? ""), ok: true)
                if c.screenSharing {
                    HStack(spacing: Theme.Space.s) {
                        look(prominent: false)
                        if screenKept { ScreenForgetButton(ref: ref, kept: $screenKept) }
                    }
                } else {
                    Self.noScreenSharing
                }
                if c.screenSharing, screenKept { unlockRow(locked: false) }
                if !apps.isEmpty { allowedApps }
            }
        } else {
            line(c.note ?? "还不知道能不能操控", ok: false)
        }
    }

    /// The way to Cua Driver's two permissions (#1280): what is allowed already, checked; the rest as numbered steps in
    /// System Settings' own words; 去授权 (macOS's dialogs), 再检查一次, and VNC to do it from the phone.
    private func grantGuide(_ c: MachineComputer, asking: Bool) -> some View {
        let steps = GrantGuide.steps(c)
        let failed = c.job.flatMap { $0.kind == "grant" && $0.state == "failed" ? $0.error : nil }
        return VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.s) {
                if asking {
                    ProgressView().controlSize(.small)
                } else {
                    Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution)
                }
                Text(failed.map { "没能请求授权：\($0)" } ?? GrantGuide.header(c, asking: asking))
                    .font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true)
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("computer-status")
            ForEach(Array(steps.enumerated()), id: \.element.id) { _, step in
                Label {
                    Text(step.text).font(Theme.Font.label).foregroundStyle(step.done ? Color.secondary : Color.primary)
                        .fixedSize(horizontal: false, vertical: true)
                } icon: {
                    if step.done {
                        Image(systemName: Theme.Icon.success).foregroundStyle(Theme.positive)
                    } else {
                        Text("\(GrantGuide.number(of: step, in: steps))").font(Theme.Font.labelStrong.monospacedDigit()).foregroundStyle(Theme.accent)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("computer-step-\(step.id)")
                .accessibilityValue(step.done ? "已完成" : "待做")
            }
            HStack(spacing: Theme.Space.s) {
                if !asking { action("去授权", "grant", prominent: !c.screenSharing, identifier: "computer-grant") }
                if c.screenSharing { look(prominent: true) }
                Button { Task { await recheck() } } label: {
                    if checking { ProgressView().controlSize(.small) } else { Text("再检查一次").font(Theme.Font.labelStrong).lineLimit(1) }
                }
                .themedButton()
                .disabled(checking)
                .accessibilityIdentifier("computer-recheck")
            }
            if !c.screenSharing { Self.noScreenSharing }
        }
    }

    private func line(_ text: String, ok: Bool) -> some View {
        Label {
            Text(text).font(Theme.Font.secondary).foregroundStyle(ok ? Color.primary : Color.secondary)
                .fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: ok ? Theme.Icon.success : Theme.Icon.error).foregroundStyle(ok ? Theme.positive : Theme.caution)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("computer-status")
    }

    private func action(_ title: String, _ what: String, prominent: Bool = false, identifier: String) -> some View {
        Button { Task { await run(what) } } label: {
            Text(title).font(Theme.Font.labelStrong).lineLimit(1)
        }
        .themedButton(prominent: prominent)
        .disabled(asking)
        .accessibilityIdentifier(identifier)
    }

    /// VNC: the Mac's live screen, where macOS's dialogs can be answered from the phone too.
    private func look(prominent: Bool) -> some View {
        Button {
            model.openScreen(ref)
        } label: {
            Label("VNC", systemImage: Theme.Icon.screen).font(Theme.Font.labelStrong).lineLimit(1)
        }
        .themedButton(prominent: prominent)
        .accessibilityIdentifier("computer-screen")
    }

    static let noScreenSharing = Text("在那台 Mac 的「系统设置 › 通用 › 共享」里打开「屏幕共享」，这里就能看它的画面、远程点「允许」。")
        .font(Theme.Font.label).foregroundStyle(.secondary)

    // MARK: Calls

    private func recheck() async {
        checking = true
        defer { checking = false }
        await refresh()
    }

    private func refresh() async {
        guard let api = model.api(ref.group) else { return }
        if let c = try? await api.computer(machine: ref.id) { fresh = c }
        if let a = try? await api.computerApps(machine: ref.id) { apps = a }
    }

    #if os(macOS)
    static let oneTapTitle = "在这台 Mac 上一键解锁"
    #else
    static let oneTapTitle = "在这台手机上一键解锁"
    #endif

    /// 一键解锁 on this device (it needs the sign-in kept), and 锁上 while it is on (#1507 a-2).
    private func unlockRow(locked: Bool) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            Toggle(isOn: Binding(get: { oneTap }, set: { on in
                if on {
                    askOneTap = true
                } else {
                    ScreenUnlock.set(false, for: ref)
                    oneTap = false
                }
            })) {
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(Self.oneTapTitle).font(Theme.Font.body).lineLimit(1).minimumScaleFactor(0.8)
                    Text("锁屏时验证一下，就把记住的密码输入它的锁屏").font(Theme.Font.label).foregroundStyle(.secondary)
                }
            }
            .accessibilityIdentifier("computer-one-tap")
            if oneTap, !locked {
                Button { Task { await run("lock") } } label: {
                    Label("锁上", systemImage: Theme.Icon.locked).font(Theme.Font.labelStrong).lineLimit(1)
                }
                .themedButton()
                .disabled(asking)
                .accessibilityIdentifier("computer-lock")
            }
        }
        .padding(.top, Theme.Space.xs)
    }

    /// Face ID (or the passcode), then the live screen signs in and types the kept password on the lock screen.
    private func unlockNow() async {
        let name = model.machine(ref)?.name ?? ref.id
        guard await ScreenUnlock.authenticate(name) else { return }
        model.openScreen(ref, unlock: true)
    }

    /// The apps always allowed here (#1507): each can be taken back, and is asked about again the next time.
    private var allowedApps: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text("已允许助手操作的 App").font(Theme.Font.label).foregroundStyle(.secondary)
            ForEach(apps) { app in
                HStack(spacing: Theme.Space.s) {
                    Text(app.name).font(Theme.Font.secondary).lineLimit(1)
                    Spacer(minLength: Theme.Space.s)
                    Button { Task { await remove(app) } } label: {
                        Text("移除").font(Theme.Font.labelStrong).lineLimit(1)
                    }
                    .themedButton()
                    .accessibilityIdentifier("computer-app-remove")
                }
                .accessibilityElement(children: .contain)
                .accessibilityIdentifier("computer-app-\(app.bundleID)")
            }
        }
        .padding(.top, Theme.Space.xs)
    }

    private func remove(_ app: ComputerApp) async {
        guard let api = model.api(ref.group) else { return }
        do {
            apps = try await api.removeComputerApp(machine: ref.id, bundleID: app.bundleID)
            Haptics.success()
        } catch {
            if let m = error.shown { model.toast = Toast("移除失败：\(m)", style: .error) }
        }
    }

    private func set(_ on: Bool) async {
        guard let api = model.api(ref.group) else { return }
        saving = true
        defer { saving = false }
        do {
            fresh = try await api.setComputer(machine: ref.id, enabled: on)
            Haptics.success()
        } catch {
            if let m = error.shown { model.toast = Toast((on ? "打开" : "关掉") + "失败：\(m)", style: .error) }
        }
    }

    private func run(_ what: String) async {
        guard let api = model.api(ref.group) else { return }
        asking = true
        defer { asking = false }
        do {
            fresh = try await api.computerJob(machine: ref.id, what)
        } catch {
            if let m = error.shown { model.toast = Toast(m, style: .error) }
        }
    }
}

/// The steps to Cua Driver's permissions on that Mac (#1280), checked against macOS 26's System Settings: 隐私与安全性 ›
/// 辅助功能 and › 录屏与系统录音 (屏幕录制 before), where the app is listed as Cua Driver (com.trycua.driver).
nonisolated enum GrantGuide {
    struct Step: Hashable, Identifiable {
        var id: String
        var done: Bool
        var text: String
    }

    /// What is allowed already first, then what is left to do, then 再检查一次.
    static func steps(_ c: MachineComputer) -> [Step] {
        let accessibility = c.accessibility == "granted", screen = c.screenRecording == "granted"
        var done: [Step] = [], todo: [Step] = []
        if accessibility {
            done.append(Step(id: "accessibility", done: true, text: "辅助功能：已允许 Cua Driver"))
        } else {
            todo.append(Step(id: "accessibility", done: false, text: "在那台 Mac 上打开「系统设置 › 隐私与安全性 › 辅助功能」，打开「Cua Driver」的开关"))
        }
        if screen {
            done.append(Step(id: "screen", done: true, text: "录屏与系统录音：已允许 Cua Driver"))
        } else {
            todo.append(Step(id: "screen", done: false, text: accessibility
                ? "在那台 Mac 上打开「系统设置 › 隐私与安全性 › 录屏与系统录音」（旧版 macOS 叫「屏幕录制」），打开「Cua Driver」的开关"
                : "同一处的「录屏与系统录音」（旧版 macOS 叫「屏幕录制」）里，也打开「Cua Driver」的开关"))
            todo.append(Step(id: "prompt", done: false, text: "系统要是提示退出并重新打开 Cua Driver，或问它能否直接访问屏幕，都选允许"))
        }
        todo.append(Step(id: "recheck", done: false, text: "回到这里点「再检查一次」"))
        return done + todo
    }

    /// A step's number among the ones left to do.
    static func number(of step: Step, in steps: [Step]) -> Int {
        (steps.filter { !$0.done }.firstIndex(of: step) ?? 0) + 1
    }

    static func header(_ c: MachineComputer, asking: Bool) -> String {
        if asking { return "那台 Mac 上弹出了授权窗口，按下面的步骤允许" }
        let missing = [c.accessibility, c.screenRecording].filter { $0 != "granted" }.count
        return missing == 2 ? "Cua Driver 还需要两项权限" : "Cua Driver 还需要一项权限"
    }
}
