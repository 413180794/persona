import PersonaKit
import SwiftUI

/// The Sign In section of an extension's page (#4507), under its status: how it is signed in (Not signed in / Waiting for
/// confirmation / Signed in: <account> / Expired / Error: <reason>), Sign In or Sign In Again, and Sign Out when signed in. Signing in is a card a step at a time: a QR code
/// to scan (with its link, which opens the app it belongs to on this phone), a page to open with the code to enter there
/// (and the link's QR code, for another phone), or fields to fill; it closes when the hub says done.
struct ExtensionAuthSection: View {
    var auth: ExtensionAuth
    /// The extension is switched on: its program can be asked.
    var enabled: Bool
    var model: ExtensionSignInModel
    @State private var confirmLogout = false

    var body: some View {
        Section {
            status
            if let p = model.prompt {
                card(p)
            } else {
                actions
            }
            if let e = model.error, !e.isEmpty {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled)
                    .accessibilityIdentifier("extension-auth-error")
            }
        } header: {
            SettingsHeading(String(localized: "登录"))
        } footer: {
            Text(footer)
        }
        // A link, code or QR code on screen: the hub is asked again until the sign-in is done or the step changes.
        .task(id: model.prompt.flatMap(ExtensionSignIn.pollKey)) {
            _ = await model.waitForAuthorization()
        }
        .confirmationDialog("退出\(auth.title)？", isPresented: $confirmLogout, titleVisibility: .visible) {
            Button(role: .destructive) { Task { await model.logout() } } label: { Text("退出登录").lineLimit(1) }
                .accessibilityIdentifier("extension-auth-logout-confirm")
            Button("取消", role: .cancel) {}
        } message: {
            Text("扩展存的登录凭据会删掉，要用时再登录。")
        }
    }

    private var footer: String {
        if !enabled { return String(localized: "启用后才能登录。") }
        if let d = auth.description, !d.isEmpty { return d }
        return String(localized: "登录凭据只存在 hub 上这个扩展自己的目录里。")
    }

    private var status: some View {
        HStack(spacing: Theme.Space.m) {
            Image(systemName: icon).resizable().scaledToFit().foregroundStyle(tone)
                .scaledFrame(width: Theme.Size.iconS, height: Theme.Size.iconS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(auth.title.isEmpty ? String(localized: "登录") : auth.title).font(Theme.Font.body).lineLimit(1)
                Text(ExtensionAuthText.state(auth)).font(Theme.Font.label)
                    .foregroundStyle(auth.state == .error ? Theme.danger : auth.needsSignIn ? Theme.cautionText : Color.secondary)
                    .lineLimit(2)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("extension-auth-status")
        .accessibilityValue(auth.state.rawValue)
    }

    private var icon: String {
        switch auth.state {
        case .signedIn: Theme.Icon.successFilled
        case .error: Theme.Icon.errorFilled
        case .pending: Theme.Icon.time
        default: Theme.Icon.infoFilled
        }
    }

    private var tone: Color {
        switch auth.state {
        case .signedIn: Theme.positive
        case .error: Theme.danger
        case .signedOut, .expired: Theme.caution
        case .pending, .unknown: .secondary
        }
    }

    @ViewBuilder private var actions: some View {
        HStack(spacing: Theme.Space.s) {
            Button {
                Task { await model.start() }
            } label: {
                Group {
                    if model.busy { ProgressView().controlSize(.small) } else { Text(ExtensionAuthText.signInButton(auth)).lineLimit(1) }
                }
                .formActionWidth()
            }
            .themedButton(prominent: !auth.isSignedIn)
            .disabled(!enabled || model.busy)
            .accessibilityIdentifier("extension-auth-sign-in")
            if auth.isSignedIn {
                Button(role: .destructive) { confirmLogout = true } label: {
                    Text("退出登录").lineLimit(1).formActionWidth()
                }
                .themedButton()
                .disabled(!enabled || model.busy)
                .accessibilityIdentifier("extension-auth-logout")
            }
        }
        .padding(.vertical, Theme.Space.xs)
    }

    /// One step of the sign-in.
    @ViewBuilder private func card(_ p: ConnectorPrompt) -> some View {
        if let hint = p.hint, !hint.isEmpty {
            Text(hint).font(Theme.Font.secondary).foregroundStyle(.secondary)
        }
        if let e = p.error, !e.isEmpty {
            Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger)
                .accessibilityIdentifier("extension-auth-step-error")
        }
        if ExtensionSignIn.isQR(p) {
            ExtensionSignInQR(rows: p.qr?.rows ?? [])
            waiting(String(localized: "等你扫码确认…"))
            buttons(primary: ExtensionSignIn.link(p).map { url in
                (String(localized: "在本机打开"), "extension-auth-open", { UIApplication.shared.open(url) })
            })
        } else if ExtensionSignIn.isDeviceCode(p) {
            if let code = p.userCode, !code.isEmpty { userCode(code) }
            if let rows = p.qr?.rows, !rows.isEmpty {
                // The link's QR code, to open it on another phone.
                ExtensionSignInQR(rows: rows)
            }
            waiting(String(localized: "等你在打开的页面上确认…"))
            buttons(primary: ExtensionSignIn.link(p).map { url in
                (String(localized: "打开登录页面"), "extension-auth-open", { UIApplication.shared.open(url) })
            })
        } else if ExtensionSignIn.isForm(p) {
            ForEach(p.fields ?? []) { field($0) }
            buttons(primary: (model.busy ? String(localized: "正在验证…") : String(localized: "下一步"), "extension-auth-next", {
                Task { await model.next() }
            }), primaryDisabled: !model.canSend)
        } else {
            Text("这一步这个版本的 App 显示不了。更新 App 后点「重新开始」再试。")
                .font(Theme.Font.secondary).foregroundStyle(Theme.cautionText)
                .accessibilityIdentifier("extension-auth-cannot-draw")
            buttons(primary: (String(localized: "重新开始"), "extension-auth-restart", { Task { await model.restart() } }))
        }
    }

    private func waiting(_ text: String) -> some View {
        HStack(spacing: Theme.Space.s) {
            ProgressView().controlSize(.small)
            Text(text).font(Theme.Font.secondary).foregroundStyle(.secondary).lineLimit(1)
        }
    }

    /// The code to enter on the page: large, copyable.
    private func userCode(_ code: String) -> some View {
        HStack {
            Text(code).font(Theme.Font.display).monospaced().textSelection(.enabled).lineLimit(1).minimumScaleFactor(0.6)
                .accessibilityIdentifier("extension-auth-user-code")
            Spacer(minLength: Theme.Space.s)
            Button {
                UIPasteboard.general.string = code
                Haptics.success()
            } label: {
                Label("复制", systemImage: Theme.Icon.copy).labelStyle(.iconOnly)
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("复制验证码")
        }
    }

    /// The step's button (when it has one) and Cancel, side by side in the card.
    private func buttons(primary: (String, String, () -> Void)?, primaryDisabled: Bool = false) -> some View {
        HStack(spacing: Theme.Space.s) {
            if let primary {
                Button(action: primary.2) { Text(primary.0).lineLimit(1).formActionWidth() }
                    .themedButton(prominent: true)
                    .disabled(primaryDisabled)
                    .accessibilityIdentifier(primary.1)
            }
            Button { model.cancel() } label: { Text("取消").lineLimit(1).formActionWidth() }
                .themedButton()
                .accessibilityIdentifier("extension-auth-cancel")
        }
        .padding(.vertical, Theme.Space.xs)
    }

    @ViewBuilder private func field(_ f: ConnectorPrompt.Field) -> some View {
        let text = Binding(get: { model.values[f.name] ?? "" }, set: { model.values[f.name] = $0 })
        VStack(alignment: .leading, spacing: Theme.Space.xs) {
            Text(f.label).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            #if os(iOS) || os(visionOS)
            if f.secret == true {
                SecretField(f.placeholder ?? f.label, filled: String(localized: "已填写"), text: text, identifier: "extension-auth-field-" + f.name)
            } else {
                TextField(f.label, text: text, prompt: Text(f.placeholder ?? f.label))
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier("extension-auth-field-" + f.name)
            }
            #else
            Group {
                if f.secret == true {
                    SecureField(f.label, text: text, prompt: Text(f.placeholder ?? f.label))
                } else {
                    TextField(f.label, text: text, prompt: Text(f.placeholder ?? f.label))
                }
            }
            .autocorrectionDisabled()
            .accessibilityIdentifier("extension-auth-field-" + f.name)
            #endif
        }
    }
}

/// A sign-in's QR code, centred in the card.
private struct ExtensionSignInQR: View {
    var rows: [String]

    var body: some View {
        QRCodeView(rows: rows)
            .scaledFrame(width: Theme.Size.box200, height: Theme.Size.box200)
            .frame(maxWidth: .infinity)
            .accessibilityIdentifier("extension-auth-qr")
    }
}
