import PersonaSSH
import SwiftUI

/// Settings › hub › SSH Tunnel (docs/network.md「经 SSH 隧道连 hub」): one paired hub's tunnel, with what it is doing now.
struct HubSSHTunnelPage: View {
    @Environment(AppModel.self) private var model
    let hubID: String

    var body: some View {
        if let hub = model.registry.hubs.first(where: { $0.id == hubID }) {
            SSHTunnelForm(saved: hub.ssh, onSave: { model.setSSH($0, hub: hubID) }) {
                if hub.ssh?.isUsable == true { status(hub) }
            }
            .detailTitle(String(localized: "SSH 隧道"))
        } else {
            EmptyStateView(symbol: Theme.Icon.sshTunnel, title: String(localized: "这台 hub 已经断开"), message: String(localized: "回到 设置 › hub 重新配对。"))
        }
    }

    private func status(_ hub: PairedHub) -> some View {
        let tunnels = SSHTunnels.shared
        let routed = tunnels.routed.contains(hub.id)
        let live = model.isLive(hub.group)
        let now: String = routed ? (live ? String(localized: "正在经 SSH 隧道连 hub") : String(localized: "正在经 SSH 隧道连接…"))
            : live ? String(localized: "没有使用：直连 hub 正常") : String(localized: "未连接")
        return Section {
            SettingsRow(title: now, symbol: routed ? Theme.Icon.sshTunnel : Theme.Icon.server, tint: routed ? Theme.positive : Theme.Hue.gray)
                .accessibilityIdentifier("ssh.status")
            if case .down(let why) = tunnels.states[hub.id] {
                Text("上次失败：\(why)").font(Theme.Font.caption).foregroundStyle(Theme.danger)
            }
        } header: {
            SettingsHeading(String(localized: "状态"))
        }
    }
}

/// The SSH tunnel's settings, on Settings › hub › SSH Tunnel and on the pairing page (before there is a hub): this device's
/// public key to copy onto the server, the server and the hub's address from there, Always Use SSH. Saving tests the whole
/// way first; the server's key is shown for the user to confirm the first time, and a changed one is refused until the
/// user trusts it again — never taken silently.
struct SSHTunnelForm<Status: View>: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    /// What is kept now (nil: none yet).
    var saved: SSHRoute?
    /// The route to keep (tested, its server's key confirmed); nil: remove it.
    var onSave: (SSHRoute?) -> Void
    /// The pairing page goes back to pairing once it is saved.
    var dismissOnSave = false
    @ViewBuilder var status: () -> Status

    // Empty port and target fields mean their defaults (shown as the prompt).
    @State private var host = ""
    @State private var port = ""
    @State private var user = ""
    @State private var targetHost = ""
    @State private var targetPort = ""
    @State private var always = false
    /// The server's key as confirmed (from what is saved, or just now).
    @State private var pinned: (fingerprint: String, algorithm: String)?
    @State private var loaded = false
    @State private var checking = false
    @State private var problem: String?
    @State private var confirm: Confirm?
    @State private var removing = false
    @State private var key: SSHIdentity?
    @State private var keyProblem: String?
    /// The fields as the form opened: in its Mac sheet Cancel asks first once they differ.
    @State private var opened: [String]?
    private var fields: [String] { [host, port, user, targetHost, targetPort, always ? "1" : "0"] }

    private enum Confirm: Identifiable {
        case unknown(fingerprint: String, algorithm: String)
        case changed(expected: String, actual: String)
        var id: String {
            switch self {
            case .unknown(let fp, _): "unknown" + fp
            case .changed(_, let fp): "changed" + fp
            }
        }
    }

    var body: some View {
        Form {
            status()
            keySection
            Section {
                field(String(localized: "主机"), $host, prompt: "ssh.example.com", id: "ssh.host")
                field(String(localized: "端口"), $port, prompt: "22", digits: true, id: "ssh.port")
                field(String(localized: "用户名"), $user, prompt: String(localized: "user 或 user:target"), id: "ssh.user")
            } header: {
                SettingsHeading(String(localized: "SSH 服务器"))
            } footer: {
                Text("这台设备能访问、它又能访问 hub 的 SSH 服务器。Warpgate 这类堡垒机的用户名写成 user:target。")
            }
            Section {
                field(String(localized: "地址"), $targetHost, prompt: SSHRoute.defaultTargetHost, id: "ssh.targetHost")
                field(String(localized: "端口"), $targetPort, prompt: String(SSHRoute.defaultTargetPort), digits: true, id: "ssh.targetPort")
            } header: {
                SettingsHeading(String(localized: "隧道目标"))
            } footer: {
                Text("从 SSH 服务器上看 hub 的地址。hub 就在这台服务器上时用 127.0.0.1:18660。")
            }
            if let pinned {
                Section {
                    Label {
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text(pinned.algorithm.isEmpty ? "已确认" : "\(pinned.algorithm) · 已确认").lineLimit(1)
                            Text(pinned.fingerprint).font(Theme.Font.code).foregroundStyle(.secondary).lineLimit(1)
                                .truncationMode(.middle).textSelection(.enabled)
                        }
                    } icon: {
                        Image(systemName: Theme.Icon.hostKeyTrusted).foregroundStyle(Theme.positive)
                    }
                    .accessibilityIdentifier("ssh.hostKey")
                } header: {
                    SettingsHeading(String(localized: "服务器指纹"))
                } footer: {
                    Text("第一次连接时确认过。之后服务器的指纹变了会拒绝连接，不会自动接受。")
                }
            }
            Section {
                Toggle(isOn: $always) { Text("总是走 SSH").lineLimit(1) }
                    .accessibilityIdentifier("ssh.always")
            } footer: {
                #if os(macOS)
                Text("关闭时先直连 hub，连不上才走 SSH 隧道。隧道断开后会自动重试。")
                #else
                Text("关闭时先直连 hub，连不上才走 SSH 隧道。iPhone 回到前台时隧道会重新建立。")
                #endif
            }
            if let problem {
                Section {
                    Label {
                        Text(problem).font(Theme.Font.secondary).fixedSize(horizontal: false, vertical: true)
                    } icon: {
                        Image(systemName: Theme.Icon.error).foregroundStyle(Theme.danger)
                    }
                    .accessibilityIdentifier("ssh.problem")
                }
            }
            #if os(macOS)
            macActions
            #else
            Section {
                Button { Task { await testAndSave() } } label: {
                    HStack(spacing: Theme.Space.s) {
                        if checking { ProgressView() }
                        Text(checking ? "正在测试…" : "测试连接并保存").lineLimit(1).minimumScaleFactor(0.8)
                    }
                    .frame(maxWidth: .infinity)
                }
                .themedButton(prominent: true)
                .disabled(checking || draft == nil)
                .accessibilityIdentifier("ssh.save")
                .listRowBackground(Color.clear)
                .listRowInsets(EdgeInsets())
            } footer: {
                Text("会用上面的设置登录 SSH 服务器，并打开一次到隧道目标的转发。")
            }
            if saved != nil {
                Section {
                    Button(role: .destructive) { removing = true } label: {
                        SettingsRow(title: String(localized: "删除 SSH 隧道"), symbol: Theme.Icon.delete, tint: Theme.danger)
                    }
                    .modifier(SettingsButtonLook())
                    .bareListRow()
                }
            }
            #endif
        }
        .themedPage()
        #if os(macOS)
        .modifier(MacFormChrome(inSheet: dismissOnSave, unsaved: opened.map { $0 != fields } ?? false,
                                cancel: { dismiss() }, save: saveButton))
        #endif
        .onAppear(perform: load)
        .onChange(of: host) { _, _ in serverChanged() }
        .onChange(of: port) { _, _ in serverChanged() }
        .onChange(of: always) { _, now in alwaysChanged(now) }
        .alert(confirmTitle, isPresented: Binding(get: { confirm != nil }, set: { if !$0 { confirm = nil } }), presenting: confirm) { c in
            switch c {
            case .unknown(let fp, let alg):
                Button("信任并保存") { Task { await testAndSave(trusting: (fp, alg)) } }
            case .changed(_, let fp):
                Button("信任新指纹", role: .destructive) { Task { await testAndSave(trusting: (fp, pinned?.algorithm ?? "")) } }
            }
            Button("取消", role: .cancel) {}
        } message: { c in
            switch c {
            case .unknown(let fp, let alg):
                Text("\(alg) \(fp)\n\n在服务器上运行 ssh-keygen -lf 查看它的主机公钥，指纹一致再信任。之后指纹变了会拒绝连接。")
            case .changed(let expected, let actual):
                Text("钉住的：\(expected)\n现在的：\(actual)\n\n可能是服务器重装或换了密钥，也可能有人在中间。确认是服务器换了密钥再信任。")
            }
        }
        .confirmationDialog("删除 SSH 隧道？", isPresented: $removing, titleVisibility: .visible) {
            Button("删除", role: .destructive) { onSave(nil); dismiss() }
        } message: {
            Text("之后只直连 hub。服务器上的那行 authorized_keys 需要你自己删。")
        }
    }

    private var confirmTitle: String {
        switch confirm {
        case .changed: String(localized: "SSH 服务器的指纹变了")
        default: String(localized: "确认 SSH 服务器的指纹")
        }
    }

    // MARK: This device's key

    @ViewBuilder private var keySection: some View {
        Section {
            if let key {
                let line = key.publicKeyLine(comment: SSHKeys.comment)
                VStack(alignment: .leading, spacing: Theme.Space.xs) {
                    Text(line).font(Theme.Font.code).lineLimit(1).truncationMode(.middle).textSelection(.enabled)
                    Text(key.fingerprint).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1).truncationMode(.middle)
                }
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("ssh.publicKey")
                #if os(macOS)
                // Standard push buttons side by side, as a Mac form has them, not two tinted rows (#1170).
                HStack(spacing: Theme.Space.s) {
                    Spacer(minLength: 0)
                    Button { copy(line) } label: { Label("复制公钥", systemImage: Theme.Icon.sshKey).lineLimit(1) }
                        .accessibilityIdentifier("ssh.copyKey")
                    Button { copy(authorizedKeysLine(key)) } label: {
                        Label("复制 authorized_keys 行", systemImage: Theme.Icon.copy).lineLimit(1)
                    }
                    .accessibilityIdentifier("ssh.copyAuthorizedKeys")
                }
                .buttonStyle(.bordered)
                #else
                Button { copy(line) } label: {
                    SettingsRow(title: String(localized: "复制公钥"), symbol: Theme.Icon.sshKey, tint: Theme.accent)
                }
                .modifier(SettingsButtonLook())
                .accessibilityIdentifier("ssh.copyKey")
                .bareListRow()
                Button { copy(authorizedKeysLine(key)) } label: {
                    SettingsRow(title: String(localized: "复制 authorized_keys 行"), symbol: Theme.Icon.copy, tint: Theme.accent)
                }
                .modifier(SettingsButtonLook())
                .accessibilityIdentifier("ssh.copyAuthorizedKeys")
                .bareListRow()
                #endif
            } else if let keyProblem {
                Text(keyProblem).font(Theme.Font.secondary).foregroundStyle(Theme.danger)
            }
        } header: {
            SettingsHeading(String(localized: "这台设备的公钥"))
        } footer: {
            Text("把「authorized_keys 行」加到 SSH 服务器上这个用户的 ~/.ssh/authorized_keys：它带着 restrict 和 permitopen，这把钥匙只能转发到下面的隧道目标，不能登录 shell。私钥只存在这台设备的钥匙串里。")
        }
    }

    private func authorizedKeysLine(_ key: SSHIdentity) -> String {
        key.authorizedKeysLine(targetHost: targetHostValue, targetPort: Int(targetPort) ?? SSHRoute.defaultTargetPort, comment: SSHKeys.comment)
    }

    #if os(macOS)
    // MARK: Mac buttons

    /// Test Connection and Save as a Mac button: the sheet's default button in its bar, or a standard one at the page's end.
    private var saveButton: some View {
        Button { Task { await testAndSave() } } label: {
            HStack(spacing: Theme.Space.s) {
                if checking { ProgressView().controlSize(.small) }
                Text(checking ? "正在测试…" : "测试连接并保存").lineLimit(1)
            }
        }
        .disabled(checking || draft == nil)
        .accessibilityIdentifier("ssh.save")
    }

    /// The page's own buttons at the end of the form (in a sheet, saving is in its bar): standard ones at the trailing
    /// edge, not full-width tinted capsules (#1170).
    @ViewBuilder private var macActions: some View {
        if saved != nil || !dismissOnSave {
        Section {
            HStack(spacing: Theme.Space.s) {
                if saved != nil {
                    Button(role: .destructive) { removing = true } label: { Text("删除 SSH 隧道…").lineLimit(1) }
                        .buttonStyle(.bordered)
                }
                Spacer(minLength: 0)
                if !dismissOnSave { saveButton.buttonStyle(.borderedProminent) }
            }
        } footer: {
            Text("会用上面的设置登录 SSH 服务器，并打开一次到隧道目标的转发。")
        }
        }
    }
    #endif

    private func copy(_ s: String) {
        UIPasteboard.general.string = s
        Haptics.tap()
        model.toast = Toast(String(localized: "已复制"), style: .success)
    }

    // MARK: Fields

    @ViewBuilder
    private func field(_ title: String, _ text: Binding<String>, prompt: String, digits: Bool = false, id: String) -> some View {
        #if os(macOS)
        TextField(title, text: text, prompt: Text(verbatim: prompt))
            .autocorrectionDisabled()
            .accessibilityIdentifier(id)
        #else
        LabeledContent {
            TextField(title, text: text, prompt: Text(verbatim: prompt))
                .multilineTextAlignment(.trailing)
                .keyboardType(digits ? .numberPad : .asciiCapable)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .accessibilityIdentifier(id)
        } label: {
            Text(title).lineLimit(1)
        }
        #endif
    }

    private var targetHostValue: String {
        let t = targetHost.trimmingCharacters(in: .whitespaces)
        return t.isEmpty ? SSHRoute.defaultTargetHost : t
    }

    /// A port field: its number, the default when empty, nil when it is not a port.
    private static func portValue(_ s: String, default d: Int) -> Int? {
        let t = s.trimmingCharacters(in: .whitespaces)
        guard !t.isEmpty else { return d }
        guard let p = Int(t), (1...65535).contains(p) else { return nil }
        return p
    }

    /// What the fields say, with the key confirmed for this server; nil while a port is not a number.
    private var draft: SSHRoute? {
        let h = host.trimmingCharacters(in: .whitespaces), u = user.trimmingCharacters(in: .whitespaces)
        guard !h.isEmpty, !u.isEmpty, let p = Self.portValue(port, default: 22),
              let tp = Self.portValue(targetPort, default: SSHRoute.defaultTargetPort) else { return nil }
        return SSHRoute(host: h, port: p, user: u, targetHost: targetHostValue, targetPort: tp, hostKey: pinned?.fingerprint,
                        hostKeyAlgorithm: pinned?.algorithm, always: always)
    }

    private func load() {
        if key == nil {
            do { key = try SSHKeys.identity() } catch { keyProblem = error.localizedDescription }
        }
        guard !loaded else { return }
        loaded = true
        defer { opened = fields }
        guard let saved else { return }
        host = saved.host
        port = saved.port == 22 ? "" : String(saved.port)
        user = saved.user
        targetHost = saved.targetHost == SSHRoute.defaultTargetHost ? "" : saved.targetHost
        targetPort = saved.targetPort == SSHRoute.defaultTargetPort ? "" : String(saved.targetPort)
        always = saved.always
        if let fp = saved.hostKey { pinned = (fp, saved.hostKeyAlgorithm ?? "") }
    }

    /// Another server: the key confirmed was the old one's.
    private func serverChanged() {
        guard loaded, let pinnedFor = saved, pinned != nil else { return }
        if host.trimmingCharacters(in: .whitespaces) != pinnedFor.host || Self.portValue(port, default: 22) != pinnedFor.port { pinned = nil }
    }

    /// Always Use SSH on a tunnel already saved, nothing else changed: kept right away (no test needed for it).
    private func alwaysChanged(_ now: Bool) {
        guard loaded, var kept = saved, kept.isUsable, kept.always != now, var d = draft else { return }
        d.always = kept.always
        guard d == kept else { return }
        kept.always = now
        onSave(kept)
    }

    // MARK: Test and save

    private func testAndSave(trusting key: (fingerprint: String, algorithm: String)? = nil) async {
        guard var route = draft else { return }
        if let key {
            route.hostKey = key.fingerprint
            route.hostKeyAlgorithm = key.algorithm
        }
        checking = true
        problem = nil
        defer { checking = false }
        do {
            let seen = try await SSHTunnels.shared.verify(route)
            route.hostKey = seen.fingerprint
            route.hostKeyAlgorithm = seen.algorithm
            pinned = seen
            onSave(route)
            Haptics.success()
            model.toast = Toast(String(localized: "SSH 隧道已保存"), style: .success)
            if dismissOnSave { dismiss() }
        } catch SSHTunnelError.hostKeyUnknown(let fp, let alg) {
            Haptics.attention()
            confirm = .unknown(fingerprint: fp, algorithm: alg)
        } catch SSHTunnelError.hostKeyChanged(let expected, let actual) {
            Haptics.error()
            confirm = .changed(expected: expected, actual: actual)
        } catch {
            Haptics.error()
            problem = Self.advice(error, route)
        }
    }

    /// What went wrong and what to do about it.
    static func advice(_ error: Error, _ route: SSHRoute) -> String {
        guard let e = error as? SSHTunnelError else { return error.localizedDescription }
        let target = route.targetHost.contains(":") ? "[\(route.targetHost)]:\(route.targetPort)" : "\(route.targetHost):\(route.targetPort)"
        return switch e {
        case .notAuthorized:
            String(localized: "SSH 服务器没有接受这台设备的公钥。把「复制 authorized_keys 行」拷的那一行加到服务器上 \(route.user) 的 ~/.ssh/authorized_keys（Warpgate：加到这个用户的公钥里），再试。")
        case .targetRefused(let why):
            String(localized: "登录成功，但服务器打不开到 \(target) 的转发（\(why)）。检查隧道目标、hub 是否在这个端口上，以及 authorized_keys 里 permitopen 的地址和这里一致。")
        case .unreachable(let why):
            String(localized: "连不上 \(route.host):\(String(route.port))（\(why)）。检查地址和端口，以及这台设备能不能访问它。")
        case .handshake(let why):
            String(localized: "SSH 握手失败（\(why)）。服务器需要支持 Ed25519 或 ECDSA 主机密钥和 AES-GCM 加密。")
        default:
            e.localizedDescription
        }
    }
}

extension SSHTunnelForm where Status == EmptyView {
    init(saved: SSHRoute?, dismissOnSave: Bool = false, onSave: @escaping (SSHRoute?) -> Void) {
        self.init(saved: saved, onSave: onSave, dismissOnSave: dismissOnSave, status: { EmptyView() })
    }
}

#if os(macOS)
/// The SSH form's frame on the Mac: a sheet's size and bar (Cancel, and saving as the default button), or the page's
/// grouped form in the detail column.
private struct MacFormChrome<Save: View>: ViewModifier {
    var inSheet: Bool
    var unsaved: Bool
    var cancel: () -> Void
    var save: Save

    func body(content: Content) -> some View {
        if inSheet {
            content
                .macSheet()
                .sheetCancel(unsaved: unsaved, cancel: cancel)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) { save }
                }
        } else {
            content.formStyle(.grouped)
        }
    }
}
#endif
