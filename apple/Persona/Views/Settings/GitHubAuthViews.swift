import SwiftUI

// 设置 › 连接器 › GitHub's sign-in (docs/connectors.md「二 · 7」): where the token comes from — one of the user's
// computers' gh login, or the device flow with the GitHub CLI's built-in OAuth App — chosen here and kept on the hub (hub.json's
// github.auth.from_machine is only the default). Choosing one leaves the other, so there is no 退出登录. When the token
// in use lacks a scope the connector needs, the page says which and what does not work.

/// GET /api/v1/connectors/github/auth.
nonisolated struct GitHubAuth: Codable, Equatable, Sendable {
    nonisolated struct Missing: Codable, Equatable, Sendable {
        var scope: String
        var affects: [String]
    }

    nonisolated struct Machine: Codable, Equatable, Sendable, Identifiable {
        var id: String
        var name: String
        var state: String // signed_in | not_signed_in | no_gh | too_old | offline | error
        var account: String?
        var scopes: [String]?
        var missing: [Missing]?
        var error: String?
    }

    var source: String // machine | device
    var machine: String?
    var machines: [Machine]
    var scopes: [String]?
    var scopesKnown: Bool?
    var missing: [Missing]?

    enum CodingKeys: String, CodingKey {
        case source, machine, machines, scopes, missing
        case scopesKnown = "scopes_known"
    }

    /// The scopes a token lacks, as people read them: repo、notifications.
    static func names(_ missing: [Missing]) -> String { missing.map(\.scope).joined(separator: "、") }
}

extension APIClient {
    func githubAuth() async throws -> GitHubAuth { try await get("/api/v1/connectors/github/auth") }

    /// Chooses where GitHub's token comes from: a machine's gh login, or the device flow (machine nil).
    func setGitHubAuth(machine: String?) async throws -> GitHubAuth {
        struct Body: Encodable, Sendable { var source: String; var machine: String? }
        let body = Body(source: machine == nil ? "device" : "machine", machine: machine)
        return try JSON.decoder.decode(GitHubAuth.self, from: await request("PUT", "/api/v1/connectors/github/auth", body: body))
    }
}

/// GitHub's sections under its status: the switch, where to sign in from, the device flow when chosen, what its
/// token cannot do, and deleting what it brought in.
struct GitHubConnectorSections: View {
    var connector: HubConnector
    var reload: () async -> Void
    @Environment(AppModel.self) private var app
    @State private var model = ConnectorSignInModel(id: "github")
    @State private var auth: GitHubAuth?
    /// The row being chosen (a machine's id, "device"), while the hub applies it.
    @State private var choosing: String?

    var body: some View {
        ConnectorSwitchSection(connector: connector, model: model, reload: reload)
        sourceSection
        if auth?.source == "device", connector.isEnabled, !connector.isSignedIn {
            ConnectorSignInSteps(connector: connector, model: model, reload: { await reload(); await load() },
                                 footer: "设备码登录的授权加密存在 hub 上；改用电脑上的 gh 登录时会忘掉。")
        }
        if let auth, let missing = auth.missing, !missing.isEmpty, connector.isSignedIn {
            scopeSection(auth, missing)
        }
        ConnectorFeedsSection(connector: connector, model: model, reload: reload)
        ConnectorDataSection(connector: connector, model: model, reload: reload, showsLogout: false)
    }

    private var sourceSection: some View {
        Section {
            if let auth {
                ForEach(auth.machines) { m in
                    row(key: m.id, icon: Theme.Icon.machine, title: m.name, subtitle: subtitle(m),
                        warn: m.state != "signed_in" || !(m.missing ?? []).isEmpty, selected: auth.source == "machine" && auth.machine == m.id) {
                        choose(machine: m.id)
                    }
                }
                row(key: "device", icon: Theme.Icon.deviceCode, title: "设备码登录", subtitle: "不用电脑：在 GitHub 上输入验证码授权",
                    warn: false, selected: auth.source == "device") {
                    choose(machine: nil)
                }
            } else {
                HStack(spacing: Theme.Space.s) {
                    ProgressView()
                    Text("正在看各台电脑上的 gh 登录…").font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
                }
            }
        } header: {
            SettingsHeading("从哪里登录")
        } footer: {
            Text("选一台电脑就用它上面 gh 的登录；设备码登录不依赖任何电脑。换一个就是退出上一个。")
        }
        .task(id: app.config) { await load() }
    }

    private func row(key: String, icon: String, title: String, subtitle: String, warn: Bool, selected: Bool,
                     action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: Theme.Space.m) {
                Image(systemName: icon).foregroundStyle(.secondary)
                    .scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(title).font(Theme.Font.body).foregroundStyle(Color.primary).lineLimit(1)
                    Text(subtitle).font(Theme.Font.label).foregroundStyle(warn ? Theme.cautionText : Color.secondary).lineLimit(2)
                }
                Spacer(minLength: Theme.Space.s)
                if choosing == key {
                    ProgressView()
                } else if selected {
                    Image(systemName: Theme.Icon.done).foregroundStyle(Theme.accent)
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .disabled(choosing != nil)
        .accessibilityIdentifier("github.source." + key)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private func subtitle(_ m: GitHubAuth.Machine) -> String {
        switch m.state {
        case "signed_in":
            let who = "gh 已登录" + (m.account.map { " · " + $0 } ?? "")
            if let missing = m.missing, !missing.isEmpty { return who + " · 少 " + GitHubAuth.names(missing) + " 权限" }
            return who
        case "not_signed_in": return "gh 没有登录"
        case "no_gh": return "没装 gh"
        case "too_old": return "jarvisd 太旧，读不了 gh 的登录"
        case "offline": return "离线"
        default: return "读不了 gh 的登录" + (m.error.map { "：" + $0 } ?? "")
        }
    }

    @ViewBuilder private func scopeSection(_ auth: GitHubAuth, _ missing: [GitHubAuth.Missing]) -> some View {
        let names = GitHubAuth.names(missing)
        let flags = missing.map(\.scope).joined(separator: ",")
        let machine = auth.machines.first { $0.id == auth.machine }?.name ?? "这台电脑"
        Section {
            Label {
                Text(auth.source == "device"
                     ? "设备码登录少了 \(names) 权限：先换成别的登录方式，再用设备码重新登录一次。"
                     : "\(machine) 的 gh 少了 \(names) 权限：在那台电脑上运行 gh auth refresh -s \(flags)，或者改用设备码登录。")
                    .font(Theme.Font.body).textSelection(.enabled)
            } icon: {
                Image(systemName: Theme.Icon.error).foregroundStyle(Theme.caution)
            }
            .accessibilityIdentifier("github.missingScopes")
            ForEach(missing.flatMap(\.affects), id: \.self) { a in
                Text(a).font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
        } header: {
            SettingsHeading("权限不够")
        }
    }

    private func load() async {
        guard let api = app.api else { return }
        do {
            auth = try await api.githubAuth()
        } catch {
            app.show(error)
        }
    }

    private func choose(machine: String?) {
        guard let api = app.api else { return }
        choosing = machine ?? "device"
        Task {
            defer { choosing = nil }
            do {
                auth = try await api.setGitHubAuth(machine: machine)
                model.cancel()
                await reload()
            } catch {
                app.show(error)
            }
        }
    }
}
