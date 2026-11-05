import CryptoKit
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
    /// the watch an answer-only one at its first hub. A call while one runs waits for it (launch, the watch asking and
    /// 同步到 Apple Watch can come together; each would get the watch a token of its own).
    func upgradeCredentials() async {
        if let running = credentialsUpgrade { return await running.value }
        let run = Task { await upgradeCredentialsOnce() }
        credentialsUpgrade = run
        await run.value
        credentialsUpgrade = nil
    }

    private func upgradeCredentialsOnce() async {
        guard isPaired, !isDemo else { return }
        #if DEBUG
        if UserDefaults.standard.string(forKey: "pairURL") != nil { return } // simulator runs paired from the command line
        #endif
        for paired in registry.hubs where paired.deviceID == nil {
            let old = paired.config
            guard let t = try? await APIClient(config: old).createToken(name: UIDevice.current.name, kind: Self.credentialKind) else { continue }
            adoptDeviceToken(hub: paired.id, old: old.token, token: t.token, deviceID: t.id)
        }
        #if canImport(WatchConnectivity)
        if WatchSync.shared.watchAppInstalled {
            for g in registry.groups {
                guard let primary = registry.primary(g), primary.watch?.url != primary.config.url,
                      let t = try? await APIClient(config: primary.config).createToken(name: "Apple Watch", kind: "watch") else { continue }
                updateHub(primary.id) { $0.watch = ServerConfig(url: primary.config.url, token: t.token) }
            }
        }
        #endif
        pushWatchConfigs()
    }

    /// Hub `id` traded the pairing token `old` for this device's own. The registry keeps the new one, and every socket
    /// still on the old token goes on with it at once: whatever address or route it took (a candidate address, a mesh
    /// peer, the SSH tunnel), whichever group the hub is in by now (a hello may regroup it while the trade is out).
    /// Comparing whole configs missed those, and the socket stayed on the pairing token until the hub dropped it after
    /// its grace (docs/protocol.md「临时配对」): 401 on every attempt, 连接中… for good (#2766).
    func adoptDeviceToken(hub id: String, old: String, token: String, deviceID: String) {
        tradedTokens[old] = token
        // nil: paired again with another link meanwhile; that pairing trades its own.
        if let r = DeviceCredentialTrade.registry(registry, hub: id, old: old, token: token, deviceID: deviceID) {
            registry = r
            saveRegistry(r)
            buildLinks()
        }
        for link in links where !link.isDemo { link.live.renewCredential(from: old, to: token) }
    }

    /// Nothing kept about this device's credentials outside the registry any more (the registry goes with `unpair`).
    func forgetDeviceCredentials() {
        UserDefaults.standard.removeObject(forKey: HubStore.legacyDeviceIDKey)
    }
}

/// Trading a pairing token for the device's own (pure, for the tests).
nonisolated enum DeviceCredentialTrade {
    /// The registry once hub `id` has `token` (and `deviceID`) in place of `old`; nil when the hub is gone or no longer
    /// on `old` (paired again with another link while the trade was out).
    static func registry(_ r: HubRegistry, hub id: String, old: String, token: String, deviceID: String) -> HubRegistry? {
        guard let i = r.hubs.firstIndex(where: { $0.id == id }), r.hubs[i].config.token == old else { return nil }
        var out = r
        out.hubs[i].config.token = token
        out.hubs[i].deviceID = deviceID
        out.hubs[i].tradedPairing = fingerprint(old)
        return out
    }

    /// What the registry keeps of a traded pairing token: its SHA-256, never the token.
    static func fingerprint(_ token: String) -> String {
        SHA256.hash(data: Data(token.utf8)).map { String(format: "%02x", $0) }.joined()
    }

    /// The credential a request or socket on `config` goes with: a traded pairing token comes back as the device's
    /// token, at the same address.
    static func current(_ config: ServerConfig, traded: [String: String]) -> ServerConfig {
        guard let token = traded[config.token], token != config.token else { return config }
        var c = config
        c.token = token
        return c
    }

    /// A pairing link whose token was already traded for `hub`'s own credential (the same link handled twice): the hub
    /// keeps the device token instead of going back to the pairing one.
    static func keepsDevice(_ hub: PairedHub, pairing: ServerConfig, traded: [String: String]) -> Bool {
        guard hub.deviceID != nil, pairing.token != hub.config.token else { return false }
        if let token = traded[pairing.token] { return hub.config.token == token }
        return hub.tradedPairing == fingerprint(pairing.token) // traded at an earlier launch
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
