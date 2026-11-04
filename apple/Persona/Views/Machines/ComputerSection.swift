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
    }

    // MARK: State

    @ViewBuilder private var state: some View {
        let c = shown
        if !online {
            line("机器离线，连上后才能操控", ok: false)
        } else if let job = c.job, job.running {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                HStack(spacing: Theme.Space.s) {
                    ProgressView().controlSize(.small)
                    Text(job.kind == "grant" ? Self.grantHint : (job.step ?? "正在安装"))
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
        } else if c.daemon, !c.missingPermissions.isEmpty {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                if let job = c.job, job.kind == "grant", job.state == "done" {
                    line(Self.grantHint, ok: false)
                    action("再请求一次", "grant", identifier: "computer-grant")
                } else {
                    let failed = c.job.flatMap { $0.kind == "grant" && $0.state == "failed" ? $0.error : nil }
                    line(failed ?? "Cua Driver 还没有\(c.missingPermissions.joined(separator: "、"))权限", ok: false)
                    action("去授权", "grant", prominent: true, identifier: "computer-grant")
                }
            }
        } else if c.ready {
            line("现在可以操控" + (c.version.map { " · Cua Driver \($0)" } ?? ""), ok: true)
        } else {
            line(c.note ?? "还不知道能不能操控", ok: false)
        }
    }

    /// macOS shows its permission dialogs on the Mac itself: nobody can answer them from the phone (#1184 adds its live
    /// screen).
    static let grantHint = "那台 Mac 上弹出了授权窗口：在它的屏幕上点「允许」"

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

    // MARK: Calls

    private func refresh() async {
        guard let api = model.api(ref.group) else { return }
        if let c = try? await api.computer(machine: ref.id) { fresh = c }
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
