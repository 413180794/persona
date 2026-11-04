import SwiftUI

/// 操控电脑 on a Mac's page while the hub's plugin cua is on (docs/computer-use.md): the switch that lets the assistant
/// operate it through Cua Driver, and whether it can now, or what is missing. The node is asked again when the page
/// opens.
struct ComputerSection: View {
    @Environment(AppModel.self) private var model
    let ref: MachineRef
    var computer: MachineComputer
    var online: Bool
    /// What the hub said since the page opened (newer than the machine list's).
    @State private var fresh: MachineComputer?
    @State private var saving = false

    private var shown: MachineComputer { fresh ?? computer }

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
                    status
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .card()
        }
        .task(id: online && shown.enabled) {
            if online && shown.enabled { await refresh() }
        }
    }

    private var status: some View {
        let ready = shown.ready && online
        return Label {
            Text(statusText).font(Theme.Font.secondary).foregroundStyle(ready ? Color.primary : Color.secondary)
                .fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: ready ? Theme.Icon.success : Theme.Icon.error).foregroundStyle(ready ? Theme.positive : Theme.caution)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("computer-status")
    }

    private var statusText: String {
        let c = shown
        if !online { return "机器离线，连上后才能操控" }
        if c.ready { return "现在可以操控" + (c.version.map { " · Cua Driver \($0)" } ?? "") }
        return c.note ?? "还不知道能不能操控"
    }

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
}
