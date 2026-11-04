import SwiftUI

/// 浏览器（ego lite） on a Mac's page while the hub's plugin ego is on (docs/plan-ego-browser.md): the switch that lets
/// the assistant drive its ego lite, and how far that is: 安装 ego lite when it is not there; its first-run setup as
/// steps (open it on the Mac or over its live screen, import a browser's data, finish); 就绪 in green, with what is left
/// (its usage statistics) ticked or not. The node is asked again when the page opens, and every few seconds while a job
/// runs or the setup is under way.
struct BrowserSection: View {
    @Environment(AppModel.self) private var model
    let ref: MachineRef
    var browser: MachineBrowser
    var online: Bool
    @State private var fresh: MachineBrowser?
    @State private var saving = false
    @State private var asking = false
    @State private var checking = false

    private var shown: MachineBrowser { fresh ?? browser }

    /// Asked again every few seconds: a job runs, or the setup was opened on the Mac and is not done yet.
    private var watching: Bool {
        guard online, shown.enabled else { return false }
        if shown.job?.running == true { return true }
        return shown.installed && !shown.setUp && shown.job?.kind == "open"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            Text("浏览器").font(Theme.Font.heading).padding(.horizontal, Theme.Space.xs)
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                Toggle(isOn: Binding(get: { shown.enabled }, set: { on in Task { await set(on) } })) {
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text("允许助手用这台的浏览器").font(Theme.Font.body).lineLimit(1).minimumScaleFactor(0.8)
                        Text("经 ego lite 打开网页、点按、填表、截图").font(Theme.Font.label).foregroundStyle(.secondary)
                    }
                }
                .disabled(saving || !online)
                .accessibilityIdentifier("browser-switch")
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
    }

    // MARK: State

    @ViewBuilder private var state: some View {
        let b = shown
        if !online {
            line("机器离线，连上后才能用", ok: false)
        } else if let job = b.job, job.running, job.kind == "install" {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                HStack(spacing: Theme.Space.s) {
                    ProgressView().controlSize(.small)
                    Text(job.step ?? "正在安装").font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true)
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("browser-status")
                action("取消", "cancel", identifier: "browser-cancel")
            }
        } else if !b.installed {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                if let job = b.job, job.kind == "install", job.state == "failed" {
                    line("没装上：\(job.error ?? "安装出错了")", ok: false)
                } else {
                    line("这台 Mac 上还没有装 ego lite", ok: false)
                }
                Text("从 ego 官网下载安装包，核对 Citro Labs 的签名和苹果的公证后装进「应用程序」。")
                    .font(Theme.Font.label).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                action(b.job?.state == "failed" ? "重新安装" : "安装 ego lite", "install", prominent: true, identifier: "browser-install")
            }
        } else if !b.setUp {
            setUpGuide(b)
        } else if !b.ready {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                line(b.note ?? "ego-browser 还没跑通", ok: false)
                recheckButton
            }
        } else {
            readyList(b)
        }
    }

    /// Its first-run setup, done on the Mac: numbered steps in ego's own words, 打开 ego lite, VNC, 再检查一次.
    private func setUpGuide(_ b: MachineBrowser) -> some View {
        let steps = BrowserGuide.setUpSteps(b)
        return VStack(alignment: .leading, spacing: Theme.Space.s) {
            HStack(spacing: Theme.Space.s) {
                if b.job?.running == true { ProgressView().controlSize(.small) } else {
                    Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution)
                }
                Text(b.job?.kind == "open" ? "那台 Mac 上打开了 ego lite，按下面的步骤做完首次设置" : "ego lite 已经装好，还要做完首次设置")
                    .font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true)
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("browser-status")
            stepList(steps)
            HStack(spacing: Theme.Space.s) {
                action("打开 ego lite", "open", prominent: !b.screenSharing, identifier: "browser-open")
                if b.screenSharing { look(prominent: true) }
                recheckButton
            }
        }
    }

    /// 就绪 in green, then what else there is: the browser data imported, its usage statistics.
    private func readyList(_ b: MachineBrowser) -> some View {
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            line("就绪 · 助手能用这台的浏览器" + (b.version.map { " · ego lite \($0)" } ?? ""), ok: true)
            stepList(BrowserGuide.readySteps(b))
            if b.analytics == "on" { recheckButton }
        }
    }

    private func stepList(_ steps: [GrantGuide.Step]) -> some View {
        ForEach(steps) { step in
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
            .accessibilityIdentifier("browser-step-\(step.id)")
            .accessibilityValue(step.done ? "已完成" : "待做")
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
        .accessibilityIdentifier("browser-status")
    }

    private func action(_ title: String, _ what: String, prominent: Bool = false, identifier: String) -> some View {
        Button { Task { await run(what) } } label: {
            Text(title).font(Theme.Font.labelStrong).lineLimit(1)
        }
        .themedButton(prominent: prominent)
        .disabled(asking)
        .accessibilityIdentifier(identifier)
    }

    private var recheckButton: some View {
        Button { Task { await recheck() } } label: {
            if checking { ProgressView().controlSize(.small) } else { Text("再检查一次").font(Theme.Font.labelStrong).lineLimit(1) }
        }
        .themedButton()
        .disabled(checking)
        .accessibilityIdentifier("browser-recheck")
    }

    /// VNC: the Mac's live screen, where ego's setup is done from the phone.
    private func look(prominent: Bool) -> some View {
        Button {
            model.openScreen(ref)
        } label: {
            Label("VNC", systemImage: Theme.Icon.screen).font(Theme.Font.labelStrong).lineLimit(1)
        }
        .themedButton(prominent: prominent)
        .accessibilityIdentifier("browser-screen")
    }

    // MARK: Calls

    private func refresh() async {
        guard let api = model.api(ref.group) else { return }
        if let b = try? await api.browser(machine: ref.id) { fresh = b }
    }

    private func recheck() async {
        checking = true
        defer { checking = false }
        await refresh()
    }

    private func set(_ on: Bool) async {
        guard let api = model.api(ref.group) else { return }
        saving = true
        defer { saving = false }
        do {
            fresh = try await api.setBrowser(machine: ref.id, enabled: on)
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
            fresh = try await api.browserJob(machine: ref.id, what)
        } catch {
            if let m = error.shown { model.toast = Toast(m, style: .error) }
        }
    }
}

/// ego lite's first-run setup and what is left once it is ready, as steps (checked on 0.6.2, 2026-11-04).
nonisolated enum BrowserGuide {
    static func setUpSteps(_ b: MachineBrowser) -> [GrantGuide.Step] {
        [
            GrantGuide.Step(id: "open", done: false, text: "点「打开 ego lite」，它会在那台 Mac 上打开（屏幕共享开着时可以用「VNC」在手机上操作）"),
            GrantGuide.Step(id: "import", done: false, text: "首次设置里选从 Chrome（或别的浏览器）导入数据：网站的登录状态会一起带过来。macOS 会多次要钥匙串密码，每次都输那台 Mac 的登录密码。不想导入可以跳过"),
            GrantGuide.Step(id: "finish", done: false, text: "做完首次设置，它会装上 ego-browser 命令"),
            GrantGuide.Step(id: "recheck", done: false, text: "回到这里点「再检查一次」"),
        ]
    }

    static func readySteps(_ b: MachineBrowser) -> [GrantGuide.Step] {
        var out = [GrantGuide.Step(id: "setup", done: true, text: "首次设置做完了")]
        out.append(b.imported
            ? GrantGuide.Step(id: "import", done: true, text: "已导入另一个浏览器的数据，助手能用那些网站的登录")
            : GrantGuide.Step(id: "import", done: true, text: "没有导入别的浏览器的数据：要用的网站先在 ego lite 里登录一次"))
        switch b.analytics {
        case "on":
            out.append(GrantGuide.Step(id: "analytics", done: false, text: "关掉 ego 的使用统计：在那台 Mac 的 ego lite 里打开「设置 › 同步和服务」，关掉「Automatically sends usage statistics and crash reports to Citro Labs」"))
        case "off":
            out.append(GrantGuide.Step(id: "analytics", done: true, text: "ego 的使用统计已关"))
        default:
            break
        }
        return out
    }
}
