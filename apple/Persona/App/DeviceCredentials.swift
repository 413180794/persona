import SwiftUI
#if canImport(WatchConnectivity)
import WatchConnectivity
#endif

// One credential per device (docs/protocol.md「鉴权」): the pairing token opens everything, so right after pairing the
// phone trades it for a token of its own and stops keeping it, and gets the watch an answer-only one. A lost device is
// then revoked alone in 设置 › 设备 instead of re-pairing everything. Against an older hub (no /auth/tokens) the
// pairing token stays, as before; the next launch tries again.

extension AppModel {
    /// This device's token id at the hub-level group's hub (设置 › 设备 marks it 本机); nil while it still uses the
    /// pairing token there. Each paired hub has its own (docs/multi-hub.md).
    var deviceID: String? {
        guard let hub else { return nil }
        let on = hub.config
        return (registry.hubs.first { $0.group == hub.group && $0.config == on } ?? registry.primary(hub.group))?.deviceID
    }

    /// The kind of this device's own credential at the hub (POST /auth/tokens), shown in 设置 › 设备. A Mac is a
    /// desktop, not the phone (AppModel.registration): the hub keeps it as "other" (full rights, a computer's icon).
    nonisolated static var credentialKind: String {
        #if os(macOS)
        "desktop"
        #else
        "phone"
        #endif
    }

    /// Every paired hub trades its pairing token for this device's own (each hub separately), and every group gets
    /// the watch an answer-only one at its first hub.
    func upgradeCredentials() async {
        guard isPaired, !isDemo else { return }
        #if DEBUG
        if UserDefaults.standard.string(forKey: "pairURL") != nil { return } // simulator runs paired from the command line
        #endif
        for paired in registry.hubs where paired.deviceID == nil {
            let old = paired.config
            guard let t = try? await APIClient(config: old).createToken(name: UIDevice.current.name, kind: Self.credentialKind) else { continue }
            var cfg = old
            cfg.token = t.token
            updateHub(paired.id) { $0.config = cfg; $0.deviceID = t.id }
            buildLinks()
            // The group's socket goes on with the new credential if it was on this hub.
            if let link = link(paired.group), link.live.currentConfig == old || link.live.currentConfig == nil {
                link.live.disconnect()
                link.live.connect(cfg)
            }
        }
        #if canImport(WatchConnectivity)
        if WCSession.isSupported(), WCSession.default.isPaired, WCSession.default.isWatchAppInstalled {
            for g in registry.groups {
                guard let primary = registry.primary(g), primary.watch?.url != primary.config.url,
                      let t = try? await APIClient(config: primary.config).createToken(name: "Apple Watch", kind: "watch") else { continue }
                updateHub(primary.id) { $0.watch = ServerConfig(url: primary.config.url, token: t.token) }
            }
        }
        #endif
        pushWatchConfigs()
    }

    /// Nothing kept about this device's credentials outside the registry any more (the registry goes with `unpair`).
    func forgetDeviceCredentials() {
        UserDefaults.standard.removeObject(forKey: HubStore.legacyDeviceIDKey)
    }
}


/// 设置 › 设备: every device with its own credential, revocable one by one.
struct DevicesView: View {
    @Environment(AppModel.self) private var model
    @State private var tokens: [APIClient.DeviceToken] = []
    @State private var current: String?
    @State private var loaded = false
    @State private var revoking: APIClient.DeviceToken?

    var body: some View {
        SettingsForm {
            Section {
                ForEach(tokens.filter { $0.revokedAt == nil }) { t in row(t) }
            } footer: {
                Text("每台设备有自己的凭据，丢了哪台就撤销哪台，其他设备不受影响。手表只能看和回答，不能改设置、开关 Agent。")
            }
            let revoked = tokens.filter { $0.revokedAt != nil }
            if !revoked.isEmpty {
                Section("已撤销") {
                    ForEach(revoked) { t in row(t).foregroundStyle(.secondary) }
                }
            }
        }
        .overlay {
            if loaded, tokens.isEmpty {
                ContentUnavailableView("还没有设备凭据", systemImage: Theme.Icon.credentials,
                                       description: Text(model.deviceID == nil ? Self.upgradeNote : ""))
            }
        }
        .themedPage()
        .detailTitle("设备")
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog(revoking.map { "撤销「\($0.name)」？" } ?? "", isPresented: Binding(
            get: { revoking != nil }, set: { if !$0 { revoking = nil } }
        ), titleVisibility: .visible, presenting: revoking) { t in
            Button(t.id == current ? "撤销并退出配对" : "撤销", role: .destructive) { Task { await revoke(t) } }
        } message: { t in
            Text(t.id == current ? Self.revokeSelfNote : "那台设备会立刻断开，要重新配对才能用。")
        }
    }

    #if os(macOS)
    private static let upgradeNote = "hub 更新后，这台 Mac 会自动换成自己的凭据。"
    private static let revokeSelfNote = "这台 Mac 会立刻断开，要重新粘贴配对链接。"
    #else
    private static let upgradeNote = "hub 更新后，这台 iPhone 会自动换成自己的凭据。"
    private static let revokeSelfNote = "这台 iPhone 会立刻断开，要重新扫码配对。"
    #endif

    private func row(_ t: APIClient.DeviceToken) -> some View {
        HStack(spacing: Theme.Space.m) {
            Image(systemName: t.kind == "watch" ? Theme.Icon.watch : t.kind == "phone" ? Theme.Icon.phone : Theme.Icon.laptop)
                .scaledFrame(width: Theme.Size.icon)
                .foregroundStyle(Theme.accent)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                HStack(spacing: Theme.Space.sm) {
                    Text(t.name).lineLimit(1)
                    if t.id == current { Chip(text: "本机", look: .outline) }
                    if t.role == "answer" { Chip(text: "只能回答") }
                }
                Text(detail(t)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
            if t.revokedAt == nil {
                Button("撤销", role: .destructive) { revoking = t }
                    .buttonStyle(.borderless)
                    .lineLimit(1)
            }
        }
    }

    private func detail(_ t: APIClient.DeviceToken) -> String {
        if let r = t.revokedAt { return "撤销于 " + r.formatted(.relative(presentation: .named)) }
        if let s = t.lastSeen { return "最近使用 " + s.formatted(.relative(presentation: .named)) }
        return "添加于 " + (t.createdAt?.formatted(date: .abbreviated, time: .shortened) ?? "")
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            let r = try await api.deviceTokens()
            (tokens, current) = (r.tokens, r.current)
        } catch {
            model.show(error)
        }
        loaded = true
    }

    private func revoke(_ t: APIClient.DeviceToken) async {
        guard let api = model.api else { return }
        do {
            try await api.revokeToken(t.id)
            Haptics.success()
            // This device's own credential at the hub-level group's hub: that group goes (the others stay).
            if t.id == current { model.removeGroup(model.hub?.group ?? ""); return }
            await load()
        } catch {
            model.show(error)
        }
    }
}
