import SwiftUI
import WatchConnectivity

// One credential per device (docs/protocol.md「鉴权」): the pairing token opens everything, so right after pairing the
// phone trades it for a token of its own and stops keeping it, and gets the watch an answer-only one. A lost device is
// then revoked alone in 设置 › 设备 instead of re-pairing everything. Against an older hub (no /auth/tokens) the
// pairing token stays, as before; the next launch tries again.

extension AppModel {
    static let deviceIDKey = "auth.deviceID"

    /// This phone's device token id; nil while it still uses the pairing token.
    var deviceID: String? { UserDefaults.standard.string(forKey: Self.deviceIDKey) }

    func upgradeCredentials() async {
        guard var cfg = config else { return }
        #if DEBUG
        if UserDefaults.standard.string(forKey: "pairURL") != nil { return } // simulator runs paired from the command line
        #endif
        if deviceID == nil {
            do {
                let t = try await APIClient(config: cfg).createToken(name: UIDevice.current.name, kind: "phone")
                cfg.token = t.token
                Keychain.serverConfig = cfg
                UserDefaults.standard.set(t.id, forKey: Self.deviceIDKey)
                config = cfg
                live.disconnect()
                live.connect(cfg)
            } catch {
                return
            }
        }
        if WCSession.isSupported(), WCSession.default.isPaired, WCSession.default.isWatchAppInstalled,
           Keychain.watchConfig?.url != cfg.url,
           let t = try? await APIClient(config: cfg).createToken(name: "Apple Watch", kind: "watch") {
            Keychain.watchConfig = ServerConfig(url: cfg.url, token: t.token)
        }
        WatchSync.shared.push(config: Keychain.watchConfig ?? cfg)
    }

    func forgetDeviceCredentials() {
        UserDefaults.standard.removeObject(forKey: Self.deviceIDKey)
        Keychain.watchConfig = nil
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
        List {
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
                ContentUnavailableView("还没有设备凭据", systemImage: "key",
                                       description: Text(model.deviceID == nil ? "hub 更新后，这台 iPhone 会自动换成自己的凭据。" : ""))
            }
        }
        .navigationTitle("设备")
        .task { await load() }
        .refreshable { await load() }
        .confirmationDialog(revoking.map { "撤销「\($0.name)」？" } ?? "", isPresented: Binding(
            get: { revoking != nil }, set: { if !$0 { revoking = nil } }
        ), titleVisibility: .visible, presenting: revoking) { t in
            Button(t.id == current ? "撤销并退出配对" : "撤销", role: .destructive) { Task { await revoke(t) } }
        } message: { t in
            Text(t.id == current ? "这台 iPhone 会立刻断开，要重新扫码配对。" : "那台设备会立刻断开，要重新配对才能用。")
        }
    }

    private func row(_ t: APIClient.DeviceToken) -> some View {
        HStack(spacing: 12) {
            Image(systemName: t.kind == "watch" ? "applewatch" : t.kind == "phone" ? "iphone" : "laptopcomputer")
                .frame(width: 24)
                .foregroundStyle(Theme.accent)
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 6) {
                    Text(t.name).lineLimit(1)
                    if t.id == current { Chip(text: "本机", look: .outline) }
                    if t.role == "answer" { Chip(text: "只能回答") }
                }
                Text(detail(t)).font(.caption).foregroundStyle(.secondary).lineLimit(1)
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
            if t.id == current { model.unpair(); return }
            await load()
        } catch {
            model.show(error)
        }
    }
}
