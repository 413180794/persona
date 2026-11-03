import SwiftUI

/// 设置 › hub (docs/multi-hub.md「设置」): every hub this device paired with, by group. Hubs that know each other (their
/// hellos list each other) are one group, reached over one connection; separate groups are shown side by side
/// everywhere. Here: add a hub, rename a group, pick the group Persona / 收件箱 / the hub's settings show, disconnect a
/// hub or a whole group.
struct HubsView: View {
    @Environment(AppModel.self) private var model
    @State private var showPair = false
    @State private var renaming: String?
    @State private var newName = ""
    @State private var removingGroup: String?
    @State private var removingHub: PairedHub?

    var body: some View {
        List {
            ForEach(model.groupIDs, id: \.self) { g in
                Section {
                    groupRows(g)
                } header: {
                    header(g)
                } footer: {
                    Text(footer(g))
                }
            }
            Section {
                Button { showPair = true } label: {
                    SettingsRow(title: "添加 hub", symbol: Theme.Icon.add, tint: Theme.accent)
                }
                .modifier(SettingsButtonLook())
                .accessibilityIdentifier("hubs.add")
                .bareListRow()
            } footer: {
                Text("添加的 hub 和已有的 hub 互联时自动并进那一组；不互联的单独成一组，列表里合在一起显示、标明来自哪一组。")
            }
        }
        .themedPage()
        .navigationTitle("hub")
        .sheet(isPresented: $showPair) { PairView() }
        .alert("给这一组起个名字", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } })) {
            TextField("名字", text: $newName)
            Button("保存") { if let g = renaming { model.renameGroup(g, to: newName) }; renaming = nil }
            Button("取消", role: .cancel) { renaming = nil }
        } message: {
            Text("只改这台设备上的叫法；留空用 hub 自己的名字。")
        }
        .confirmationDialog(removingGroup.map { "断开「\(model.groupName($0))」？" } ?? "", isPresented: Binding(
            get: { removingGroup != nil }, set: { if !$0 { removingGroup = nil } }
        ), titleVisibility: .visible, presenting: removingGroup) { g in
            Button("断开这一组", role: .destructive) { model.removeGroup(g); removingGroup = nil }
        } message: { g in
            Text(model.groupIDs.count > 1 ? "这一组的机器、Agent 和缓存的对话会从这台设备上移除，别的组不受影响。要再用得重新配对。"
                                         : "断开后需要重新扫码配对。")
        }
        .confirmationDialog(removingHub.map { "断开「\($0.displayName)」？" } ?? "", isPresented: Binding(
            get: { removingHub != nil }, set: { if !$0 { removingHub = nil } }
        ), titleVisibility: .visible, presenting: removingHub) { h in
            Button("断开这台 hub", role: .destructive) { model.removeHub(h.id); removingHub = nil }
        } message: { _ in
            Text("只断开这一台：这一组还经组里别的 hub 连着。")
        }
    }

    @ViewBuilder private func groupRows(_ g: String) -> some View {
        let members = model.registry.members(g)
        let on = model.socket(g)?.currentConfig
        ForEach(members) { h in
            hubRow(h, inUse: h.config == on && model.isLive(g))
                .contextMenu {
                    if members.count > 1 {
                        Button(role: .destructive) { removingHub = h } label: { Label("断开这台 hub", systemImage: Theme.Icon.end) }
                    }
                }
        }
        // The group's other hubs, reached through it (listed in its mesh, not paired here).
        ForEach(meshOnly(g), id: \.hubID) { m in
            meshRow(m, inUse: model.isLive(g) && on.map { cfg in m.endpoints?.contains { $0.hasPrefix(cfg.url.absoluteString) } == true } == true)
        }
        Group {
            // Each paired hub's SSH tunnel (docs/network.md「经 SSH 隧道连 hub」).
            ForEach(members) { h in
                NavigationLink(value: SettingsPage.sshTunnel(h.id)) {
                    SettingsRow(title: members.count > 1 ? "SSH 隧道 · \(h.displayName)" : "SSH 隧道", symbol: Theme.Icon.sshTunnel,
                                tint: Theme.Hue.green, value: sshValue(h), look: .oneLine)
                }
                .accessibilityIdentifier("hubs.ssh.\(h.id)")
            }
            if model.isMultiHub, model.hub?.group != g {
                Button { model.selectHub(g) } label: {
                    SettingsRow(title: "设为当前 hub", symbol: Theme.Icon.successFilled, tint: Theme.positive)
                }
                .modifier(SettingsButtonLook())
            }
            Button { newName = model.registry.groupNames[g] ?? ""; renaming = g } label: {
                SettingsRow(title: "改名", symbol: Theme.Icon.edit, tint: Theme.Hue.gray)
            }
            .modifier(SettingsButtonLook())
            .accessibilityIdentifier("hubs.rename.\(g)")
            Button(role: .destructive) { removingGroup = g } label: {
                SettingsRow(title: model.groupIDs.count > 1 ? "断开这一组" : "断开并忘记服务器", symbol: Theme.Icon.end, tint: Theme.danger)
            }
            .modifier(SettingsButtonLook())
            .accessibilityIdentifier("hubs.remove.\(g)")
        }
        .bareListRow()
    }

    private func header(_ g: String) -> some View {
        HStack(spacing: Theme.Space.sm) {
            if let live = model.socket(g) { HubStatusDot(live: live) }
            SettingsHeading(model.groupName(g))
            if model.isMultiHub, model.hub?.group == g { Chip(text: "当前", look: .outline) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("hubs.group.\(g)")
    }

    /// What the SSH 隧道 row says: not set, used now, or there as the fallback / for every connection.
    private func sshValue(_ h: PairedHub) -> String {
        guard let route = h.ssh, route.isUsable else { return "未设置" }
        if SSHTunnels.shared.routed.contains(h.id) { return "使用中" }
        return route.always ? "总是" : "后备"
    }

    private func footer(_ g: String) -> String {
        let n = model.registry.members(g).count + meshOnly(g).count
        let via = model.tunnel(g)?.routed == true ? "（经 SSH 隧道）" : ""
        let state = model.isLive(g) ? "已连接" + via : model.socket(g)?.state == .connecting ? "连接中" + via : "未连接"
        let group = n > 1 ? "组里 \(n) 台 hub 互联，看到的是整组的机器；连着的那台掉了会换组里别的。" : ""
        let current = model.isMultiHub && model.hub?.group == g ? "Persona、收件箱和 hub 相关的设置用这一组。" : ""
        return state + "。" + group + current
    }

    private func meshOnly(_ g: String) -> [MeshHub] {
        let members = model.registry.members(g)
        let paired = Set(members.compactMap(\.hubID))
        var seen = Set<String>(), out: [MeshHub] = []
        // Not a paired hub, by id or by address (a paired hub whose hub_id the app has not learned yet).
        let urls = Set(members.map { $0.config.url.absoluteString.trimmingCharacters(in: CharacterSet(charactersIn: "/")) })
        for h in members {
            for m in h.mesh ?? [] where !paired.contains(m.hubID) && seen.insert(m.hubID).inserted {
                if m.endpoints?.contains(where: { urls.contains($0.trimmingCharacters(in: CharacterSet(charactersIn: "/"))) }) == true { continue }
                out.append(m)
            }
        }
        return out
    }

    private func hubRow(_ h: PairedHub, inUse: Bool) -> some View {
        HStack(spacing: Theme.Space.m) {
            Image(systemName: Theme.Icon.server)
                .scaledFrame(width: Theme.Size.icon)
                .foregroundStyle(Theme.accent)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                HStack(spacing: Theme.Space.sm) {
                    Text(h.displayName).lineLimit(1)
                    if inUse { Chip(text: "在用", look: .outline) }
                    if SSHTunnels.shared.routed.contains(h.id) { Chip(text: "SSH", look: .neutral) }
                }
                Text(h.config.url.absoluteString).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("hubs.hub.\(h.id)")
    }

    private func meshRow(_ m: MeshHub, inUse: Bool) -> some View {
        HStack(spacing: Theme.Space.m) {
            Image(systemName: Theme.Icon.hubGroup)
                .scaledFrame(width: Theme.Size.icon)
                .foregroundStyle(m.online == false ? Color.secondary : Theme.accent)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                HStack(spacing: Theme.Space.sm) {
                    Text(m.name ?? m.hubID).lineLimit(1)
                    Chip(text: "经互联", look: .neutral)
                    if inUse { Chip(text: "在用", look: .outline) }
                }
                Text(m.online == false ? "离线" : (m.endpoints?.first ?? "")).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }
}
