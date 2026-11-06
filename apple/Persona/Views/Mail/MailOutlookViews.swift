import AuthenticationServices
import SwiftUI

// Add Mail Account › Outlook (docs/mail.md「Outlook 接入方式」): Microsoft's sign-in for personal Outlook.com and work Microsoft
// 365 mailboxes, with a client ID the user registered in Microsoft Entra themselves (never built into the app): the
// first time, the field for it with the steps; then Sign In with Microsoft, the same flow as Google's.

/// The Outlook part of Add Mail Account: the client ID while the hub has none (or the user changes it), else the sign-in.
struct OutlookSignInSection: View {
    @Environment(AppModel.self) private var model
    @Environment(\.webAuthenticationSession) private var webAuth
    @Binding var error: String?
    var done: () -> Void
    @State private var config: APIClient.MicrosoftConfig?
    @State private var clientID = ""
    @State private var editing = false
    @State private var busy = false

    var body: some View {
        Group {
            if let config, config.configured, !editing {
                signIn(config)
            } else {
                registration
            }
        }
        .task { await load() }
    }

    private func signIn(_ config: APIClient.MicrosoftConfig) -> some View {
        Section {
            Button { Task { await signInWithMicrosoft() } } label: {
                AccountSignInLabel(mark: Theme.Brand.microsoft, title: busy ? String(localized: "正在连接 Outlook…") : String(localized: "使用 Microsoft 账号登录"), busy: busy)
            }
            .buttonStyle(.plain)
            .disabled(busy)
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets())
        } footer: {
            VStack(alignment: .leading, spacing: Theme.Space.s) {
                Text("个人的 Outlook / Hotmail 和公司的 Microsoft 365 邮箱都可以：跳到 Microsoft 登录并授权 Persona 收发邮件，不需要密码。授权失效时再登录一次即可恢复。")
                Button {
                    clientID = config.clientID ?? ""
                    editing = true
                } label: {
                    Text("更换 client ID").lineLimit(1)
                }
                .font(Theme.Font.caption)
            }
        }
    }

    private var registration: some View {
        Section {
            TextField("应用程序(客户端) ID", text: $clientID)
                .font(Theme.Font.code)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button { Task { await save() } } label: {
                if busy { ProgressView() } else { Text("保存 client ID").lineLimit(1) }
            }
            .disabled(busy || clientID.trimmingCharacters(in: .whitespaces).isEmpty)
            if let uri = config?.redirectURI {
                HStack {
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text("回调地址（注册时填）").font(Theme.Font.caption).foregroundStyle(.secondary).lineLimit(1)
                        Text(uri).font(Theme.Font.code).lineLimit(1).minimumScaleFactor(0.7).textSelection(.enabled)
                    }
                    Spacer(minLength: Theme.Space.s)
                    Button { UIPasteboard.general.string = uri; Haptics.success() } label: {
                        Image(systemName: Theme.Icon.copy)
                    }
                    .buttonStyle(.borderless)
                    .accessibilityLabel("复制回调地址")
                }
            }
        } header: {
            Text("先注册一个自己的应用")
        } footer: {
            Text("""
            Outlook 登录用你自己在 Microsoft Entra 注册的应用（免费，注册一次，这个 hub 以后都用它）：
            1. 用微软账号打开 entra.microsoft.com，「应用注册」→「新注册」；
            2. 支持的帐户类型选「任何组织目录中的帐户和个人 Microsoft 帐户」；
            3. 重定向 URI 平台选「公共客户端/本机(移动和桌面)」，填上面这个地址；
            4. 「身份验证」里把「允许公共客户端流」设为「是」；
            5. 「API 权限」添加 IMAP.AccessAsUser.All 和 SMTP.Send（委托的权限）；
            6. 把「概述」页的「应用程序(客户端) ID」填到这里。
            详细步骤见说明文档 docs/mail.md「Outlook 接入方式」。
            """)
        }
    }

    private func load() async {
        guard let api = model.api else { return }
        do {
            config = try await api.microsoftConfig()
        } catch {
            self.error = error.shown.map { String(localized: "读不到 Outlook 登录的设置：\($0)") }
        }
    }

    private func save() async {
        guard let api = model.api else { return }
        busy = true
        defer { busy = false }
        do {
            config = try await api.setMicrosoftClientID(clientID.trimmingCharacters(in: .whitespaces))
            editing = false
            error = nil
            Haptics.success()
        } catch {
            if let shown = error.shown { self.error = shown; Haptics.error() }
        }
    }

    /// Microsoft's sign-in page in the system browser; whatever it calls back with goes to the hub, which swaps a code
    /// for a refresh token, or says what to do about an error (an admin has to consent, IMAP is off…).
    private func signInWithMicrosoft() async {
        guard let api = model.api else { return }
        busy = true
        defer { busy = false }
        error = nil
        do {
            let start = try await api.microsoftSignInStart()
            let callback = try await webAuth.authenticate(using: start.authURL, callback: .customScheme(start.callbackScheme),
                                                          preferredBrowserSession: .shared, additionalHeaderFields: [:])
            let items = URLComponents(url: callback, resolvingAgainstBaseURL: false)?.queryItems ?? []
            let value = { (name: String) in items.first(where: { $0.name == name })?.value }
            _ = try await api.microsoftSignInFinish(.init(state: value("state") ?? start.state, code: value("code"),
                                                          error: value("error"), errorDescription: value("error_description")))
            Haptics.success()
            done()
        } catch let e as ASWebAuthenticationSessionError where e.code == .canceledLogin {
            return
        } catch {
            if let shown = error.shown { self.error = shown; Haptics.error() }
        }
    }
}
